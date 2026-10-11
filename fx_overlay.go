package main

import (
	"dow-mod-studio/internal/3d_engine"
	"dow-mod-studio/internal/audioplayer"
	"dow-mod-studio/internal/fda"
	"dow-mod-studio/internal/fx"
	"dow-mod-studio/internal/wav"
	"dow-mod-studio/internal/whe"
	"fmt"
	"image"
	"image/draw"
	"log"
	"math"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"dow-mod-studio/internal/modtree"
	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/paint"
	"github.com/fogleman/fauxgl"
)

// paintFxOverlay puts the overlay over a frame drawn by the hardware
// renderer, scaling it into the canvas the same way the frame itself is
// scaled so the two stay in step.
func paintFxOverlay(gtx layout.Context, overlay *image.RGBA, area image.Point) {
	if overlay == nil || area.X < 1 || area.Y < 1 {
		return
	}
	b := overlay.Bounds()
	if b.Dx() < 1 || b.Dy() < 1 {
		return
	}
	scale := float32(area.X) / float32(b.Dx())
	if s := float32(area.Y) / float32(b.Dy()); s < scale {
		scale = s
	}
	dx := (float32(area.X) - float32(b.Dx())*scale) / 2
	dy := (float32(area.Y) - float32(b.Dy())*scale) / 2
	aff := op.Affine(f32.Affine2D{}.Scale(f32.Pt(dx, dy), f32.Pt(scale, scale)))
	tr := aff.Push(gtx.Ops)
	paint.NewImageOp(overlay).Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	tr.Pop()
}

// The effects of a model are drawn over its frame rather than into it. The
// frame belongs to whichever renderer produced it — a CPU rasterisation or a
// texture the GPU is holding — and the overlay has to land on top of either
// one at the same place, so it is rasterised here into a picture of its own
// and painted over the frame by the viewer, exactly the way the frame itself
// is scaled into the canvas.
//
// A sprite is a camera facing quad at a point in model space, which after
// projection is a square on screen; its size in pixels comes from projecting a
// second point a radius to the side and measuring the distance between them,
// so a particle keeps its world size as the camera moves rather than being
// given a fixed size in pixels.

// fxGpuScene is one frame's particles, grouped by the map they read: a draw
// covers every particle sharing one texture. It is built on the CPU, where
// the simulation runs, and drawn by whichever renderer has the frame.
type fxGpuScene struct {
	Groups []fxGpuGroup
	// Textures is the decoded map of each group, keyed as the groups are:
	// a renderer that draws the scene uploads what it finds here.
	Textures map[string]*image.RGBA
}

type fxGpuGroup struct {
	Texture  string
	Additive bool
	// Origin is where the effect plays: every sprite of the group is
	// placed at its own offset from it.
	Origin  [3]float64
	Sprites []fx.Sprite
	// Beams are the ribbons this group draws. Their corners are already in
	// model space, so Origin does not apply to them: a beam is stretched
	// between two markers rather than placed at one.
	Beams []fxGpuBeam
}

// fxGpuBeam is one ribbon and the eye it is turned towards. The tessellation
// depends on the camera, so the eye travels with the ribbon.
type fxGpuBeam struct {
	Ribbon fx.BeamRibbon
	Eye    fauxgl.Vector
}

// fxSceneLight is the frame-wide light sent to the FX vertex shader, so
// sprites and ribbons share one lighting setup rather than CPU shading every
// particle separately.
type fxSceneLight struct {
	dir     fauxgl.Vector // towards the light, in model space
	ambient float64
	diffuse float64
}

// fxSceneLightOf is the light a render request asks for, with the same
// defaults the model shader falls back on.
func fxSceneLightOf(opts RenderOptions) fxSceneLight {
	l := fxSceneLight{
		dir:     opts.Light,
		ambient: opts.Ambient,
		diffuse: opts.Diffuse,
	}
	if l.dir == (fauxgl.Vector{}) {
		l.dir = fauxgl.Vector{X: 0.35, Y: -0.5, Z: 0.78}
	}
	if n := l.dir.Length(); n > 0 {
		l.dir = l.dir.Normalize()
	}
	if l.ambient == 0 && l.diffuse == 0 {
		l.ambient, l.diffuse = 0.28, 0.72
	}
	return l
}

// fxGroupKey is what one draw group is: the map it reads, the blend mode it
// draws through and the site its geometry is relative to. Sprites carry their
// position relative to their effect's site, so two effects on different
// markers cannot share a group even when they read the same map.
//
// It is a comparable struct rather than a formatted string because a busy
// frame builds one key per sprite and per ribbon: the formatting was the
// largest source of garbage the frame produced, and a map of structs hashes
// without allocating at all.
type fxGroupKey struct {
	additive bool
	texture  string
	// origin is the site's coordinates as bits, so two sites group exactly
	// when the formatted key this replaced said they did.
	origin [3]uint64
}

// fxGroupOrigin packs an effect's site the way fxGroupKey stores it.
func fxGroupOrigin(o fauxgl.Vector) [3]uint64 {
	return [3]uint64{math.Float64bits(o.X), math.Float64bits(o.Y), math.Float64bits(o.Z)}
}

// fxBuildGpuScene collects the frame's sprites and ribbons. Covering effects
// come first and additive ones last, so a glow is added over the puff it
// belongs to rather than under it.
func fxBuildGpuScene(effects []fxOverlayEffect, textures map[string]*image.RGBA, eye fauxgl.Vector, camUp fauxgl.Vector) *fxGpuScene {
	scene := &fxGpuScene{Textures: textures}
	if len(effects) == 0 {
		return nil
	}
	byKey := map[fxGroupKey]int{}
	group := func(key fxGroupKey, origin fauxgl.Vector) *fxGpuGroup {
		if i, ok := byKey[key]; ok {
			return &scene.Groups[i]
		}
		byKey[key] = len(scene.Groups)
		scene.Groups = append(scene.Groups, fxGpuGroup{
			Texture:  key.texture,
			Additive: key.additive,
			Origin:   [3]float64{origin.X, origin.Y, origin.Z},
		})
		return &scene.Groups[len(scene.Groups)-1]
	}
	for i := range effects {
		e := &effects[i]
		if e.Sim == nil {
			continue
		}
		// Build ribbons in the same model-space units. Only authored local
		// dimensions and displacement change; marker origins stay anchored.
		addRibbon := func(rb fx.BeamRibbon, preservesMarkerSpan bool) {
			rb = e.fxScaleRibbon(rb, preservesMarkerSpan)
			if rb.Alpha <= 0 || rb.Width <= 0 {
				return
			}
			name := fxTexturePath(rb.Texture)
			// Beam vertices are in model space and need no shared origin, but
			// keep their groups separate from particle groups by effect site.
			g := group(fxGroupKey{additive: rb.Additive, texture: name, origin: fxGroupOrigin(e.Origin)}, e.Origin)
			g.Beams = append(g.Beams, fxGpuBeam{Ribbon: rb, Eye: eye})
		}
		spansMarkers := e.fxSpansMarkers()
		for _, rb := range e.Sim.Beams(e.Origin, e.fxFarPoint(), spansMarkers) {
			addRibbon(rb, spansMarkers)
		}
		axX, axY, axZ := e.fxRingFrame()
		for _, rb := range e.Sim.Rings(e.Origin, axX, axY, axZ, camUp) {
			addRibbon(rb, false)
		}
		// Particles are composited after their beams. Their offsets and radii
		// use the same model-space scale as the CPU overlay path.
		for _, sp := range e.Sim.Sprites() {
			if sp.A <= 0 {
				continue
			}
			sp = fxScaleSprite(sp, e.fxScaleOrOne())
			name := fxTexturePath(sp.Texture)
			// Sprites store positions relative to their effect origin, so two
			// effects with the same texture cannot share a group if they attach
			// to different model markers — which is what the origin in the key
			// keeps apart.
			g := group(fxGroupKey{additive: sp.Additive, texture: name, origin: fxGroupOrigin(e.Origin)}, e.Origin)
			g.Sprites = append(g.Sprites, sp)
		}
	}
	if len(scene.Groups) == 0 {
		return nil
	}
	// The additive pass is recorded last, which is the order the groups are
	// drawn in and therefore the order they have to be built in.
	sortFxGroups(scene.Groups)
	return scene
}

// sortFxGroups orders the frame's draws: every covering group first, every
// additive one last, so a glow is composited over the puff it belongs to
// rather than under it.
//
// It is an insertion sort over the predicate "this group precedes that one",
// and the predicate is "covering before additive" — writing it the other way
// round silently sorts the additive groups to the front, which is what this
// did: the swap ran while the group at j was additive and the one before it
// was not, bubbling additive groups backwards.
func sortFxGroups(groups []fxGpuGroup) {
	for i := 1; i < len(groups); i++ {
		for j := i; j > 0 && !groups[j].Additive && groups[j-1].Additive; j-- {
			groups[j], groups[j-1] = groups[j-1], groups[j]
		}
	}
}

// modelFXLabel is what the button says. While the effects are playing it says
// how many there are, which is the one thing a reader cannot see from a still
// frame: eleven of them at once looks like a mistake until they are counted.
func modelFXLabel(v *modelViewer) string {
	if v == nil {
		return "FX"
	}
	switch {
	case v.showFX && len(v.fxEffects) > 0:
		return fmt.Sprintf("FX %d", len(v.fxEffects))
	case v.showFX:
		return "FX…"
	case v.fxErr != "":
		return "FX !"
	}
	return "FX"
}

// modelFXScaleStep is how much one press of the scale buttons moves it, and
// the range the control is bounded to. FX dimensions are already interpreted
// in the model's coordinate units; this control is a uniform preview adjustment.
const (
	modelFXScaleStep    = 1.25
	modelFXScaleMin     = 0.02
	modelFXScaleMax     = 4
	fxScaleDefaultValue = 1.0
)

// scaleFX multiplies the effect scale, keeping it inside its range. It is
// multiplicative so repeated presses walk the whole range evenly rather than
// bunching up at the ends.
func (v *modelViewer) scaleFX(f float64) {
	if v == nil || f <= 0 {
		return
	}
	s := v.fxScale
	if s <= 0 {
		s = fxScaleDefaultValue
	}
	s = clampFloat64(s*f, modelFXScaleMin, modelFXScaleMax)
	v.fxScale = s
	v.invalidate()
	log.Printf("3D preview: FX scale %.2f", s)
}

// modelFXScaleLabel is what the scale reads on the button row: the value, so
// it can be read off a still frame while it is being dialled in.
func modelFXScaleLabel(v *modelViewer) string {
	if v == nil {
		return "FX x1.00"
	}
	s := v.fxScale
	if s <= 0 {
		s = fxScaleDefaultValue
	}
	return fmt.Sprintf("FX x%.2f", s)
}

// toggleFX starts or stops the effects. The first press is the expensive one:
// the model's entity blueprint has to be found, its events parsed and every
// sprite map decoded, which is why it happens on a worker and the button says
// FX… until it answers. Afterwards the simulation is already built, so
// turning them off and on again costs nothing.
func (v *modelViewer) toggleFX() {
	if v == nil {
		return
	}
	if v.showFX {
		v.showFX = false
		v.fxOverlay = nil
		v.fxLast = time.Time{}
		log.Printf("3D preview: effects off")
		return
	}
	v.showFX = true
	v.fxOverlay = nil
	v.fxLast = time.Time{}
	log.Printf("3D preview: effects on (%d already loaded)", len(v.fxEffects))
}

// fxStartLoad begins resolving the model's effects, once, reporting whether it
// started anything. The path is the one the model itself was read from, which
// is how its blueprint is found.
func (ui *uiState) fxStartLoad(v *modelViewer) bool {
	if v == nil || v.model == nil || ui.modTree == nil || v.modelFile == "" {
		return false
	}
	if v.fxCh != nil || len(v.fxEffects) > 0 || v.fxErr != "" {
		return false
	}
	ch := make(chan fxLoadResult, 1)
	v.fxCh = ch
	model := v.model
	file := v.modelFile
	tree := ui.modTree
	// Whatever the sound worker decoded already is handed over, because both
	// read the same .whe and the samples would otherwise be decoded twice.
	decoded := v.fxSound.Decoded()
	go func() {
		ch <- loadModelEffects(tree, model, file, decoded)
	}()
	return true
}

// fxStartSoundLoad begins reading and decoding the model's declared sounds,
// once, reporting whether it started anything.
//
// Sounds are loaded on their own rather than riding in with the effects
// because of what they cost and when it is paid. Decoding an .fda and
// converting it into the audio context's format takes milliseconds per second
// of audio, and a walk cycle names several samples, so a sound that is decoded
// when it first plays is a sound that arrives late. Nothing about that work
// needs the FX toggle to have been pressed, and a reader may never press it, so
// the sounds are read as soon as the model is on screen and the effects stay
// behind their button.
func (ui *uiState) fxStartSoundLoad(v *modelViewer) bool {
	if v == nil || ui.modTree == nil || v.modelFile == "" {
		return false
	}
	if v.fxSoundCh != nil || v.fxSound != nil {
		return false
	}
	ch := make(chan fxLoadResult, 1)
	v.fxSoundCh = ch
	file := v.modelFile
	tree := ui.modTree
	go func() {
		ch <- loadModelSounds(tree, file)
	}()
	return true
}

// fxReload restarts both workers for a viewer with the live .whe document in
// hand: a value edited in the Events section re-resolves into the viewport
// without saving the file and without reopening the model. What is on screen
// keeps showing until the workers answer, so a reload swaps rather than
// blanks. The sound player is closed and dropped because its bundles came
// from the old definitions — fxSoundTick and the labels nil-check it, so the
// gap is silence rather than a crash — and its decoded samples are handed to
// the new loads, so only paths that changed pay for a decode.
func (ui *uiState) fxReload(v *modelViewer, doc *whe.Document) {
	if v == nil || v.model == nil || ui.modTree == nil || v.modelFile == "" {
		return
	}
	model, file, tree := v.model, v.modelFile, ui.modTree
	decoded := v.fxSound.Decoded()
	if v.fxCh == nil {
		ch := make(chan fxLoadResult, 1)
		v.fxCh = ch
		go func() {
			ch <- loadModelEffectsWhe(tree, model, file, decoded, doc)
		}()
	}
	if v.fxSoundCh == nil {
		if v.fxSound != nil {
			v.fxSound.Close()
			v.fxSound = nil
		}
		ch := make(chan fxLoadResult, 1)
		v.fxSoundCh = ch
		go func() {
			ch <- loadModelSoundsWhe(tree, file, doc, decoded)
		}()
	}
	log.Printf("3D preview: fx reload for an edited .whe (live document=%v)", doc != nil)
}

// fxApply takes the worker's answer: the effects it resolved, their maps, and
// whatever it could not read.
func (v *modelViewer) fxApply(res fxLoadResult) {
	v.fxCh = nil
	if res.err != nil {
		v.fxErr = res.err.Error()
	} else {
		v.fxErr = ""
	}
	v.fxEffects = res.effects
	v.fxTextures = res.textures
	if len(res.timelines) > 0 {
		v.fxTimelines = res.timelines
	}
	log.Printf("3D preview: effects resolved: %d effects, %d sprite textures", len(res.effects), len(res.textures))
	if v.fxErr != "" {
		log.Printf("3D preview: effects unavailable: %v", v.fxErr)
	}
	// The sounds ride in with the effects because they are read out of the same
	// .whe: the timeline that says when a footstep sounds is the one that says
	// when its dust flies. The timelines come back from the worker rather than
	// being read again here, because the .whe has already been parsed once.
	//
	// A player that already exists is left alone. The sound-only worker has
	// usually finished first, because it starts on the model's first frame
	// while this one waits for the FX button, and replacing a working player
	// with an equal one would restart any sound already playing.
	if v.fxSound == nil {
		v.fxApplySounds(res, res.timelines)
	}
	v.fxLast = time.Time{}
	v.fxOverlay = nil
}

// fxApplySoundsOnly takes the sound half of the work on its own, for the
// worker that reads the audio without resolving any effects.
func (v *modelViewer) fxApplySoundsOnly(res fxLoadResult) {
	v.fxApplySounds(res, res.timelines)
}

// fxApplySounds builds the sound player from what the worker resolved, and
// hands it the timeline so that a sound starts at the moment the clip reaches
// it rather than every frame.
func (v *modelViewer) fxApplySounds(res fxLoadResult, timelines []whe.FxTimeline) {
	// The timelines are kept whatever the sounds turned out to be: the scrub
	// bar reads them to mark the frames a clip fires something on, and a model
	// with effects but no audio would otherwise lose them.
	v.fxTimelines = timelines
	if len(res.soundBundles) == 0 {
		return
	}
	v.fxSound = fx.NewSoundPlayer(res.soundBundles, timelines, res.sounds.Snapshot())
	v.fxSoundOn = true
	log.Printf("3D preview: sounds wired: %d events, %d samples decoded",
		len(res.soundBundles), res.sounds.Len())
}

// fxSoundTick tells the sound player how far into the clip on screen the
// playhead is, which is what starts a sound at the moment its timeline names
// rather than every frame or only at the beginning.
//
// It is called every frame the clip is on, including while the clip is paused:
// a step forward over a footstep's moment is a sound a preview must make, and
// waiting for play to resume would lose it. DATAMTON event moments are in
// seconds, so the normalized playhead is converted using this clip's duration.
func (v *modelViewer) fxSoundTick(anim string) {
	if v == nil || v.fxSound == nil {
		return
	}
	v.fxSound.SetEnabled(v.fxSoundOn)
	duration := animClipSeconds(v.anim)
	v.fxSound.SetClipSeconds(duration)
	v.fxSound.Advance(anim, clampFloat64(float64(v.time)*duration, 0, duration))
}

// fxOrigins moves every effect to where its marker is under the pose on show.
// A marker on a bone follows that bone, so an effect written for a weapon
// barrel stays on the barrel while the animation runs instead of hanging in
// the air where the model was when it was pressed.
func (v *modelViewer) fxOrigins() {
	if v == nil || v.model == nil {
		return
	}
	pose := v.pose()
	for i := range v.fxEffects {
		e := &v.fxEffects[i]
		// Effects without a marker use model-space axes around their fallback
		// origin; a real marker replaces these with its animated local frame.
		e.TurnAxes = [3]fauxgl.Vector{fxAxisX, fxAxisY, fxAxisZ}
		e.TurnAngles = [3]float64{v.fxRotX * math.Pi / 180, v.fxRotY * math.Pi / 180, v.fxRotZ * math.Pi / 180}
		if world, ok := v.model.MarkerWorld(e.Marker, pose, true); ok {
			e.Origin = world
		}
		// The far end of a beam follows its own marker, which is often on
		// the same bone a few steps along, so the ribbon keeps its length
		// as the weapon swings.
		if e.HasOther {
			if world, ok := v.model.MarkerWorld(e.OtherMarker, pose, true); ok {
				e.Far = world
			}
		}
		// The marker's own frame, for a beam whose event named no second
		// marker to span to: it has to run along something, and the
		// marker's forward axis is what that is.
		if axis, ok := v.model.MarkerAxis(e.Marker, pose, true); ok {
			e.Axis = axis
		}
		if right, up, ok := v.model.MarkerBasis(e.Marker, pose, true); ok {
			e.Right = right
			e.Up = up
		}
		if x, y, z, ok := v.model.MarkerFrameAxes(e.Marker, pose, true); ok {
			e.TurnAxes = [3]fauxgl.Vector{x, y, z}
			e.Frame = [3]fauxgl.Vector{x, y, z}
		}
		// Origin and the marker frame stay untouched: each generated FX
		// displacement is turned around Origin when geometry is built.
	}
}

// Fallback axes for effects without a marker frame. Marked effects use the
// three transformed columns of their own marker matrix instead.
var (
	fxAxisX = fauxgl.Vector{X: 1}
	fxAxisY = fauxgl.Vector{Y: 1}
	fxAxisZ = fauxgl.Vector{Z: 1}
)

// fxFallbackFrame is the frame an effect with no marker of its own is laid out
// in: the file's own axes, standing in for the marker matrix the effect would
// have been played at. The third component of every vector in an effect file
// is its vertical one, and fx.FileVec carries that over to the models' Z by
// exchanging the last two components, so a marker's Y is the world's up and
// its Z the world's Y. That is the frame here, for the same reason and with
// the same swap.
//
// Standing in the world's own axes instead is what made these effects arrive
// sideways. A marker's X, Y and Z are its right, up and forward; leaving them
// as the world's right, forward and up puts the marker's up on the ground, so
// every Offset written along it becomes a sideways displacement and a ring
// lying in the marker's plane ends up standing on edge. The world's own axes
// are still the frame the correction rotation turns about, which is a
// separate thing: those are read as viewer angles, not as the file's.
//
// It is a frame in its own right, all three axes independent, which is what
// the parts that measure an Offset across two axes and take the third from
// their cross product need. Filling in the missing axes one at a time
// instead hands back three copies of one direction, and the cross product of
// two of those is zero: the Offset's third component vanishes and the panel's
// own plane has no normal to be laid out in.
var fxFallbackFrame = [3]fauxgl.Vector{
	{X: 1}, // the file's X, across
	{Z: 1}, // the file's Y, up
	{Y: 1}, // the file's Z, forward
}

// fxRotated turns v by X, then Y, then Z around the supplied fixed axes in
// marker space. Keeping each axis fixed makes the fields independent. Angles
// are radians; the pivot is supplied by the caller as a vector relative to the
// marker origin.
func fxRotated(v fauxgl.Vector, rx, ry, rz float64, axes [3]fauxgl.Vector) fauxgl.Vector {
	if rx != 0 {
		v = fxRotateAbout(v, axes[0], rx)
	}
	if ry != 0 {
		v = fxRotateAbout(v, axes[1], ry)
	}
	if rz != 0 {
		v = fxRotateAbout(v, axes[2], rz)
	}
	return v
}

// fxRotateAbout turns v through angle radians around axis, by Rodrigues'
// formula: the part of v along the axis is fixed, the part across it sweeps in
// the circle the cross product names. It is the rotation matrix written out
// without building one, which matters here because it runs for every axis of
// every effect on every frame.
func fxRotateAbout(v, axis fauxgl.Vector, angle float64) fauxgl.Vector {
	if angle == 0 {
		return v
	}
	a := axis.Normalize()
	if a == (fauxgl.Vector{}) {
		// No frame to turn about: leaving the vector alone beats turning it
		// about an arbitrary axis and calling the result a heading.
		return v
	}
	c, s := math.Cos(angle), math.Sin(angle)
	return v.MulScalar(c).
		Add(a.Cross(v).MulScalar(s)).
		Add(a.MulScalar(a.Dot(v) * (1 - c)))
}

// fxAdvance runs the simulations and rebuilds what this frame draws of them: the
// CPU overlay for the software path, and the scene the hardware renderer turns
// into billboards. It reports whether anything was drawn, so the canvas can
// keep asking for frames while the effects are playing.
//
// softwareFX says the software path is the one that will paint this frame.
// The hardware frame draws the same particles itself, so its picture is a full
// rasterisation into an image nothing will show — by far the most expensive
// thing here (a busy frame costs hundreds of milliseconds against about a
// millisecond for everything else), which is why it is skipped rather than
// built and thrown away. The flag is recomputed every frame from the same
// conditions the renderer acts on (see modelGpuUnavailable), so a hardware
// path that is off, or that breaks, has its picture back on the next frame.
func (v *modelViewer) fxAdvance(now time.Time, w, h int, opts RenderOptions, softwareFX bool) bool {
	if !v.showFX || len(v.fxEffects) == 0 || w < 1 || h < 1 {
		v.fxOverlay = nil
		v.fxScene = nil
		return false
	}
	if v.fxLast.IsZero() {
		v.fxLast = now
	}
	anim := ""
	if v.anim != nil {
		anim = v.anim.Name
	}
	// Only the effects the clip on screen fires are drawn: a .whe's timelines
	// put the build smoke and the death smoke on their own clips, and playing
	// them over an idle is what put a grey cloud above the model instead of
	// the green glow inside it.
	// The sounds move with the clip: a timeline says a footstep sounds at 0.47
	// of the way through the walk, and this is where the playhead is. It is
	// advanced before the effects are looked at, because a model may declare
	// sounds and no visible effects at all.
	v.fxSoundTick(anim)
	active := v.fxActiveEffects(anim)
	// A clip that no timeline names draws nothing, and says so once. It used to
	// fall back to playing every effect the model has, on the grounds that a
	// blank viewport is worse than a busy one. That is what took the GPU down:
	// a monolith carries 189 effects, and a clip the .whe never mentions drew
	// all of them at once — 189 beams, rings and sprays submitted every frame,
	// which a card answers by hanging and dropping the device (vkQueueSubmit
	// error -4). Showing nothing on a clip nothing drives is cheap, correct and
	// survivable; drawing everything is neither.
	if len(active) == 0 && len(v.fxEffects) > 0 && anim != "" && v.fxAnim != anim {
		log.Printf("3D preview: no timeline drives %q; it has no effects of its own", anim)
	}
	if len(active) == 0 {
		v.fxOverlay = nil
		v.fxScene = nil
		v.fxLast = now
		return false
	}
	// The clip on show changed: its effects start again from nothing, the way
	// a timeline's entry events play on the frame the clip begins.
	if v.fxAnim != anim {
		v.fxAnim = anim
		for i := range v.fxEffects {
			if v.fxEffects[i].fxPlaysFor(anim) && v.fxEffects[i].Sim != nil {
				v.fxEffects[i].Sim.Reset()
			}
		}
		v.fxLast = time.Time{}
	}
	if v.fxLast.IsZero() {
		v.fxLast = now
	}
	// The effects are stepped by the clip, not by the clock. A timeline says an
	// event fires at a point in its animation, so an effect that belongs 60% of
	// the way through a landing clip has to start there — and has to have run
	// for exactly as long as the playhead has been past that point. Stepping by
	// wall clock instead starts every effect the instant the clip begins, which
	// is what a model whose .whe was not found used to do.
	//
	// dt is the fallback for a clip the viewer is not playing: nothing is
	// moving the playhead then, and an idle preview still has to let its glows
	// breathe rather than sit frozen.
	dt := now.Sub(v.fxLast).Seconds()
	v.fxLast = now
	effects := v.fxStepOnClip(active, anim, dt)
	v.fxOrigins()
	// The scale is applied to each effect's local geometry rather than to its
	// marker origin or live simulation, so changing it keeps the effect attached
	// to the same model point without resetting particles in flight.
	scale := v.fxScale
	if scale <= 0 {
		scale = 1
	}
	effects = fxScaledEffects(effects, scale)
	// The camera's own up, which is what a billboard ring squares itself to.
	_, camUp := fxCameraBasis(opts)
	if softwareFX {
		v.fxOverlay = renderFxOverlay(effects, opts, w, h, v.fxTextures)
	} else {
		// Not this frame's picture. Keeping last frame's would paint
		// stale particles beside a hardware frame that carries its own.
		v.fxOverlay = nil
	}
	v.fxScene = fxBuildGpuScene(effects, v.fxTextures, fxEyePosition(opts), camUp)
	return true
}

// fxStepOnClip advances the simulations of the effects playing on a clip and
// returns the ones that have reached the moment their timeline fires them at.
//
// The step is the clip time that has passed since the last frame, not the wall
// clock, and an effect that belongs at t = 0.6 of the clip is neither stepped
// nor drawn until the playhead is past it. That is the difference between a
// landing sequence that opens its portal at the moment the file says and one
// that opens it the frame the animation was pressed.
//
// Three cases end up here:
//
//   - the clip changed, or the playhead went backwards (a rewind, or the clip
//     looping round): every effect starts again from nothing, and the previous
//     position is set just short of the playhead so an event written at the
//     very start counts as reached on the first frame;
//   - the clip is playing: the playhead carries the step, and an event only
//     runs once the playhead is past it;
//   - the viewer is paused mid-clip, which the FX toggle can leave it in: the
//     wall clock keeps the effects alive, since freezing a glow because the
//     reader stopped scrubbing is not what anyone wants to read.
func (v *modelViewer) fxStepOnClip(active []fxOverlayEffect, anim string, dt float64) []fxOverlayEffect {
	clipLen := 1.0
	if v.anim != nil {
		// How long one pass of the playhead actually takes, in seconds. It is
		// the frame count over the rate, not the reciprocal of the rate: the
		// playhead advances by dt*rate/frames, so it runs 0 to 1 over
		// frames/rate seconds. Using the rate alone made this twenty times too
		// short for a four frame clip, and since the step is the playhead's own
		// travel the effects crawled to a fifth of real time while the clip
		// ran — which reads as the effects freezing exactly when the animation
		// is playing, and behaving normally while it is paused, because the
		// paused path steps by the wall clock instead.
		clipLen = animClipSeconds(v.anim)
	}
	if clipLen <= 0 {
		clipLen = 1
	}
	nowClip := float64(v.time) * clipLen
	prevClip := v.fxClipPrev
	started := v.fxClipPrevAnim == anim
	// A different clip, or a playhead that moved back — which is both a rewind
	// and the clip looping — is a fresh pass, so what has run does not apply.
	if !started || nowClip+1e-9 < prevClip {
		for i := range v.fxEffects {
			if v.fxEffects[i].Sim != nil {
				v.fxEffects[i].Sim.Reset()
			}
		}
		// Just short of where the playhead is, so that an event written at the
		// start of the clip counts as reached on this very frame. Starting the
		// previous position at the playhead itself would step over it.
		prevClip = nowClip - 1e-9
	}
	v.fxClipPrev = nowClip
	v.fxClipPrevAnim = anim

	// While the clip is running the playhead is the clock. While it is paused
	// the playhead does not move, and wall time is what keeps an idle glow
	// alive; the two agree closely when it is playing, since the viewer
	// advances the playhead from the same clock.
	step := nowClip - prevClip
	if !v.playing {
		step = dt
	}
	var out []fxOverlayEffect
	for i := range active {
		e := &active[i]
		if e.Sim == nil {
			continue
		}
		fire := e.fxFireTime(anim)
		at := whe.TimeSeconds(fire, clipLen)
		// Not reached yet: it is left alone and not drawn, which is what the
		// game shows — the effect does not exist before its moment.
		if nowClip < at-1e-9 {
			continue
		}
		if step > 0 {
			e.Sim.Step(step)
		}
		out = append(out, *e)
	}
	return out
}

// fxActiveEffects are the effects that play during the named animation. The
// simulations behind every effect keep running either way, so switching back
// to a clip shows its effects already going rather than starting cold.
func (v *modelViewer) fxActiveEffects(anim string) []fxOverlayEffect {
	if len(v.fxEffects) == 0 {
		return nil
	}
	var out []fxOverlayEffect
	for i := range v.fxEffects {
		if v.fxEffects[i].fxPlaysFor(anim) {
			out = append(out, v.fxEffects[i])
		}
	}
	return out
}

// fxScaledEffects copies the effects with a uniform preview scale. The scale
// is applied to model-local positions and geometry while drawing, leaving the
// running simulations and model marker origins untouched.
func fxScaledEffects(effects []fxOverlayEffect, scale float64) []fxOverlayEffect {
	if scale == 1 {
		return effects
	}
	out := make([]fxOverlayEffect, len(effects))
	copy(out, effects)
	for i := range out {
		out[i].Scale = scale
	}
	return out
}

// fxOverlayEffect is one effect playing at one point in model space: the
// simulation, and the marker of the model it hangs off.
type fxOverlayEffect struct {
	Sim   *fx.FxSim
	Label string
	// FxName is the effect file this came from, as the event named it. It
	// is kept for the viewport's readout: an event name on its own does not
	// say which of the game's several thousand effect files was played.
	FxName string
	// Marker is the index of the model's marker the effect plays at, or
	// -1 for one with no marker to play at. Origin is where that falls
	// under the pose on screen, and is refreshed every frame because a
	// marker on a moving bone moves with it.
	Marker int
	Origin fauxgl.Vector
	// OtherMarker is the far end of a beam, -1 when the effect named no
	// second marker. Far is that marker under the pose on screen, refreshed
	// for the same reason Origin is. Spans reports whether the beam should
	// take its Length from the gap between the two rather than in model
	// units.
	//
	// HasOther is what says the far end is real. The index alone cannot:
	// zero is a valid marker, so an effect built without one set would
	// quietly span to the model's first marker.
	HasOther    bool
	OtherMarker int
	Far         fauxgl.Vector
	Spans       bool
	// Axis is where the marker's own frame points, for a beam whose event
	// named no second marker. A beam with a partner spans the two; a beam
	// without one runs this far along its Length curve. Right and Up are the
	// marker's other two axes, which is the frame a ring's Offset is written
	// in and the plane a ring with Billboard off keeps.
	Axis  fauxgl.Vector
	Right fauxgl.Vector
	Up    fauxgl.Vector
	// Frame is the marker's own X/Y/Z axes, kept apart from Right, Up and
	// Axis because those answer to different readers: Right and Up are the
	// two columns across the marker's forward axis, and Axis is that forward
	// axis. A ring lying in the marker's own plane wants all three as the
	// model wrote them.
	Frame [3]fauxgl.Vector
	// TurnAxes and TurnAngles describe the correction rotation in the marker's
	// animated frame. Vectors are rotated about Origin, which remains fixed.
	TurnAxes   [3]fauxgl.Vector
	TurnAngles [3]float64
	// Scale uniformly adjusts model-local FX positions and dimensions for the
	// preview. Zero and one both mean "as authored".
	Scale float64
	// EventScale is the fx_scale the event itself carried, applied uniformly
	// on top of Scale. Zero means the event named none.
	EventScale float64
	// Animations are the clips that fire this effect. With Timed set, an
	// effect in no clip at all plays on none of them, since something else
	// triggers it in the game.
	Animations []string
	Timed      bool
	// FireTimes is Animations again, but with the moment each clip fires this
	// event: the key is a clip name in lower case and the value the timeline's
	// own time. A clip in Animations with no entry here fires on entering it,
	// which is what a timeline written -1 means.
	//
	// It is kept beside Animations rather than inside it so that a model with
	// no timelines at all still builds: those effects have no time to honour
	// and play from the start, which is what an empty set has always meant.
	FireTimes map[string]float64
}

// fxFireTime is when this effect starts on the named clip, and whether the
// timeline said at all. A clip with no time of its own fires on entering,
// which is the -1 the game writes for "as the animation begins".
func (e *fxOverlayEffect) fxFireTime(anim string) float64 {
	if e.FireTimes != nil {
		if at, ok := e.FireTimes[strings.ToLower(anim)]; ok {
			return at
		}
	}
	return -1
}

// fxTotalScale is every scale that applies to this effect: the preview's own
// knob and the one the event was written with.
func (e *fxOverlayEffect) fxTotalScale() float64 {
	s := e.Scale
	if s <= 0 {
		s = 1
	}
	if e.EventScale > 0 {
		s *= e.EventScale
	}
	return s
}

// fxPlaysFor reports whether this effect belongs to the clip on screen. A
// model with timelines plays only what its clip fires; one without them plays
// everything, which is what an effect list from a blueprint alone means.
func (e *fxOverlayEffect) fxPlaysFor(anim string) bool {
	if !e.Timed {
		return true
	}
	if anim == "" {
		return false
	}
	for _, a := range e.Animations {
		if strings.EqualFold(a, anim) {
			return true
		}
	}
	return false
}

// fxScaleOrOne is the effect's combined preview and authored-event scale.
func (e *fxOverlayEffect) fxScaleOrOne() float64 {
	return e.fxTotalScale()
}

// fxScaleSprite scales a sprite's model-local displacement and radius together.
// A sprite with no authored radius uses the fallback in the same model units.
func fxScaleSprite(sp fx.Sprite, scale float64) fx.Sprite {
	if scale <= 0 {
		scale = 1
	}
	for i := range sp.Pos {
		sp.Pos[i] *= scale
	}
	if sp.Radius <= 0 {
		sp.Radius = fxSpriteFallbackRadius
	}
	sp.Radius *= scale
	return sp
}

// fxScaleRibbon scales authored ribbon dimensions without scaling the model
// marker gap when a beam connects two model markers. A local start offset still
// scales and translates the full beam; an unanchored beam or ring scales around
// the effect's fixed origin. Rotation is applied last so scale remains uniform
// in the effect's own frame.
func (e *fxOverlayEffect) fxScaleRibbon(rb fx.BeamRibbon, preservesMarkerSpan bool) fx.BeamRibbon {
	scale := e.fxScaleOrOne()
	if preservesMarkerSpan {
		delta := rb.From.Sub(e.Origin).MulScalar(scale - 1)
		rb.From = rb.From.Add(delta)
		rb.To = rb.To.Add(delta)
	} else {
		rb.From = e.Origin.Add(rb.From.Sub(e.Origin).MulScalar(scale))
		rb.To = e.Origin.Add(rb.To.Sub(e.Origin).MulScalar(scale))
	}
	rb.Width *= scale
	rb.Arc = rb.Arc.MulScalar(scale)
	rb.Noise *= scale
	return e.fxTurnRibbon(rb)
}

// fxSpansMarkers reports whether this beam's Length is a fraction of the gap
// between two markers of the model's own.
//
// Both halves have to hold. fx_length = distance asks for the span between the
// markers, but fire_muzzle_beam writes it alongside fx_other = target: the far
// end there is the unit that was shot, which a model viewer has no way of
// naming, so there is no gap to be a fraction of. Reading its 6.23 as a
// fraction clamps to the whole stand-in distance and leaves a stub at the
// muzzle pointing down the marker axis rather than out along the barrel.
func (e *fxOverlayEffect) fxSpansMarkers() bool {
	return e.Spans && e.HasOther
}

// fxTurnVector rotates a displacement in the marker's local frame without
// translating its pivot. The marker position itself is never passed here: the
// caller measures from it, so every turn in the viewer happens about the
// marker rather than about the model.
func (e *fxOverlayEffect) fxTurnVector(v fauxgl.Vector) fauxgl.Vector {
	return fxRotated(v, e.TurnAngles[0], e.TurnAngles[1], e.TurnAngles[2], e.TurnAxes)
}

// fxTurnRibbon rotates generated ribbon geometry around the marker. A
// billboard ring keeps facing the camera; only its centre offset moves, while
// a marker-plane ring rotates with its local frame.
func (e *fxOverlayEffect) fxTurnRibbon(rb fx.BeamRibbon) fx.BeamRibbon {
	if e.TurnAngles == [3]float64{} {
		return rb
	}
	centre := rb.From.Add(rb.To).MulScalar(0.5)
	halfSpan := rb.To.Sub(rb.From).MulScalar(0.5)
	centre = e.Origin.Add(e.fxTurnVector(centre.Sub(e.Origin)))
	if !rb.Billboard {
		halfSpan = e.fxTurnVector(halfSpan)
		if rb.Fixed {
			rb.Across = e.fxTurnVector(rb.Across)
		}
	}
	rb.From = centre.Sub(halfSpan)
	rb.To = centre.Add(halfSpan)
	rb.Arc = e.fxTurnVector(rb.Arc)
	return rb
}

// fxFarPoint is where a beam of this effect ends.
//
// An event that named a second marker is asking for the beam to span the two,
// and that is the far point. An event that named only one is not: its beam
// runs forward along the marker's own axis for the length the file wrote. The
// Length curve is a fraction in the first case and a length in model units in
// the second, which is why the two readings cannot be merged — and the run
// here is long enough to see, because a model unit on a .whm is about the
// length of a weapon.
func (e *fxOverlayEffect) fxFarPoint() fauxgl.Vector {
	if e.HasOther {
		return e.Far
	}
	axis := e.Axis
	if n := axis.Length(); n > 1e-6 {
		axis = axis.Normalize()
	} else {
		// No marker to take a direction from, so nothing says which way the
		// beam runs. Up is the only direction the files themselves keep
		// meaning: these are ground strikes written to fall out of the sky,
		// and the renderer is Z up, so a bolt of twelve units leaves the
		// monolith standing rather than lying across it. The old fallback was
		// the model's own X, which sent awakening_lighting_ground's twelve
		// unit LIGHTNING_BEAM out sideways from the middle of the building at
		// ground height.
		axis = fxAxisZ
	}
	return e.Origin.Add(axis.MulScalar(fxBeamFreeLength))
}

// fxBeamFreeLength is how far a beam with no partner marker runs, in model
// units. It stands in for the Length curve, which fxBeamSamples then reads as
// the fraction of this it actually covers.
const fxBeamFreeLength = 1.0

// fxRingFrame is the marker's own X/Y/Z, which is the frame a ring lying in
// the marker's plane is laid out in: the map's upright axis along X, its width
// along Z, and the Offset measured across Y and Z.
//
// This is deliberately not Right, Up and Axis. Those are read for the jobs
// they already do - Right and Up come from MarkerBasis, the two columns
// across the marker's forward axis, and Axis is that forward axis itself - so
// asking for the ring's frame out of them hands over the forward axis where
// the upright one belongs, and every panel that keeps its own plane comes out
// lying on its side.
// An effect whose event names no marker is laid out in fxFallbackFrame, the
// whole frame rather than per-axis stand-ins: one missing axis filled in on
// its own hands back a frame whose axes are no longer a frame, and the parts
// that take two of them and cross them for the third get a zero where a
// direction should be.
func (e *fxOverlayEffect) fxRingFrame() (axX, axY, axZ fauxgl.Vector) {
	axX, axY, axZ = e.Frame[0], e.Frame[1], e.Frame[2]
	if axX.Length() < 1e-6 || axY.Length() < 1e-6 || axZ.Length() < 1e-6 {
		return fxFallbackFrame[0], fxFallbackFrame[1], fxFallbackFrame[2]
	}
	return axX, axY, axZ
}

// fxOverlayBounds is the size the overlay is drawn at. It is the size of the
// frame it goes over, so the two line up without a second calculation.
func fxOverlayStep(effects []fxOverlayEffect, dt float64) {
	for _, e := range effects {
		if e.Sim != nil {
			e.Sim.Step(dt)
		}
	}
}

// renderFxOverlay draws one frame of every effect into a new picture of w by h
// pixels, transparent where nothing was drawn. opts is the same render request
// the frame was drawn with, so the overlay and the model share one camera.
//
// textures is keyed by the path a sprite names, which is the file's own path
// ("DATA:ART/FX/GLOW.TGA") rather than anything the caller had to invent. A
// sprite whose map is missing is still drawn, as a flat disc in its own
// colour: an effect that shows where its particles are is worth more than one
// that silently drops them.
func renderFxOverlay(effects []fxOverlayEffect, opts RenderOptions, w, h int, textures map[string]*image.RGBA) *image.RGBA {
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	if len(effects) == 0 || w < 1 || h < 1 {
		return out
	}
	view := buildViewMatrix(opts)
	// The camera's own right and up, so that a quad drawn along them faces
	// the eye square on. They come out of the same orbit the frame is drawn
	// with: the direction from the target to the eye, and the Z-up the
	// models are authored in.
	right, up := fxCameraBasis(opts)
	eye := fxEyePosition(opts)
	for _, e := range effects {
		if e.Sim == nil {
			continue
		}
		// Beams first: they are the long shapes a particle is drawn on top
		// of. Keep marker-to-marker distances fixed while scaling authored
		// dimensions and local offsets, just as in the GPU scene builder.
		spansMarkers := e.fxSpansMarkers()
		for _, rb := range e.Sim.Beams(e.Origin, e.fxFarPoint(), spansMarkers) {
			rb = e.fxScaleRibbon(rb, spansMarkers)
			if rb.Alpha > 0 && rb.Width > 0 {
				drawFxBeam(out, view, eye, w, h, rb, textures)
			}
		}
		axX, axY, axZ := e.fxRingFrame()
		for _, rb := range e.Sim.Rings(e.Origin, axX, axY, axZ, up) {
			rb = e.fxScaleRibbon(rb, false)
			if rb.Alpha > 0 && rb.Width > 0 {
				drawFxBeam(out, view, eye, w, h, rb, textures)
			}
		}
		for _, sp := range e.Sim.Sprites() {
			sp = fxScaleSprite(sp, e.fxScaleOrOne())
			world := fauxgl.Vector{
				X: e.Origin.X + sp.Pos[0],
				Y: e.Origin.Y + sp.Pos[1],
				Z: e.Origin.Z + sp.Pos[2],
			}
			if sp.Horizontal {
				drawFxHorizontalSprite(out, view, w, h, world, sp.Radius, sp, textures)
			} else {
				drawFxBillboard(out, view, right, up, w, h, world, sp.Radius, sp, textures)
			}
		}
	}
	return out
}

// fxSpriteFallbackRadius is the size, in model units, given to a sprite whose
// file carries no Radius curve.
const fxSpriteFallbackRadius = 0.05

// fxCameraBasis is where the camera's right and up point in model space, for
// the same orbit buildViewMatrix sets up. Models are authored Z up, so that
// is the up the look is taken against.
//
// The two are the same axes the view matrix is built from, and the order of
// the cross product is what makes them agree: the eye sits along dir, so
// looking at the model means looking back along -dir, and the camera's right
// is worldUp x -dir. Taking dir x worldUp instead hands back the negative of
// it — the camera's left — which squares every billboard to a frame rolled a
// half turn against the one it is being viewed through. A round puff is
// symmetric enough to hide that, but a sprite with a direction in it, which is
// what a beam's map is, comes out upside down and swings the wrong way as the
// camera orbits.
func fxCameraBasis(opts RenderOptions) (right, up fauxgl.Vector) {
	pitch := clampFloat64(opts.Pitch, -1.5, 1.5)
	dir := fauxgl.Vector{
		X: math.Cos(opts.Yaw) * math.Cos(pitch),
		Y: math.Sin(opts.Yaw) * math.Cos(pitch),
		Z: math.Sin(pitch),
	}
	worldUp := fauxgl.Vector{X: 0, Y: 0, Z: 1}
	right = worldUp.Cross(dir).Normalize()
	if n := right.Length(); n < 1e-6 {
		// Looking straight down the up axis: any sideways direction will
		// do, and the frame itself is edge on anyway.
		right = fauxgl.Vector{X: 1}
	}
	up = dir.Cross(right).Normalize()
	return right, up
}

// fxEyePosition is where the camera is in model space, for the same orbit
// buildViewMatrix sets up. A beam needs it to turn its cross-section towards
// the eye: a ribbon has a direction but no idea which way it is being looked
// at, and a beam drawn flat to the world disappears edge on.
func fxEyePosition(opts RenderOptions) fauxgl.Vector {
	return engine.EyePosition(engine.Camera{Yaw: opts.Yaw, Pitch: opts.Pitch, Distance: opts.Distance, Target: opts.Target})
}

// drawFxHorizontalSprite projects a fixed XY-plane sprite, matching the
// non-billboard STYLE_RING plane the game authors perpendicular to world Z.
func drawFxHorizontalSprite(dst *image.RGBA, view fauxgl.Matrix, w, h int, world fauxgl.Vector, radius float64, sp fx.Sprite, textures map[string]*image.RGBA) {
	if radius <= 0 {
		radius = fxSpriteFallbackRadius
	}
	c, s := math.Cos(sp.Rotation), math.Sin(sp.Rotation)
	right := fauxgl.Vector{X: c, Y: s}
	up := fauxgl.Vector{X: -s, Y: c}
	points := [4]fauxgl.Vector{
		world.Sub(right.MulScalar(radius)).Sub(up.MulScalar(radius)),
		world.Add(right.MulScalar(radius)).Sub(up.MulScalar(radius)),
		world.Add(right.MulScalar(radius)).Add(up.MulScalar(radius)),
		world.Sub(right.MulScalar(radius)).Add(up.MulScalar(radius)),
	}
	projected := [4]*fauxgl.Vector{}
	for i := range points {
		projected[i] = fxProject(view, points[i], w, h)
		if projected[i] == nil {
			return
		}
	}
	verts := [4]fx.BeamVert{{U: 0, V: 1}, {U: 1, V: 1}, {U: 1, V: 0}, {U: 0, V: 0}}
	drawFxBeamQuad(dst, projected[0], projected[1], projected[3], projected[2],
		verts[0], verts[1], verts[3], verts[2], textures[sp.Texture],
		clampFloat64(sp.R, 0, 1)*clampFloat64(sp.A, 0, 1),
		clampFloat64(sp.G, 0, 1)*clampFloat64(sp.A, 0, 1),
		clampFloat64(sp.B, 0, 1)*clampFloat64(sp.A, 0, 1),
		clampFloat64(sp.A, 0, 1), sp.Additive, w, h)
}

// drawFxBillboard projects one sprite and fills the square it covers.
func drawFxBillboard(dst *image.RGBA, view fauxgl.Matrix, right, up fauxgl.Vector, w, h int, world fauxgl.Vector, radius float64, sp fx.Sprite, textures map[string]*image.RGBA) {
	centre := fxProject(view, world, w, h)
	if centre == nil {
		return
	}
	// The size on screen: a second point a radius to the right of the sprite,
	// projected the same way. Perspective then keeps the sprite the size its
	// distance asks for.
	edge := fxProject(view, world.Add(right.MulScalar(radius)), w, h)
	if edge == nil {
		return
	}
	half := math.Hypot(edge.X-centre.X, edge.Y-centre.Y)
	if half < 0.5 {
		// Smaller than a pixel: one pixel of it is closer to what the
		// effect looks like than none of it.
		half = 0.5
	}
	if half > float64(maxInt(w, h)) {
		return
	}
	x0 := int(math.Floor(centre.X - half))
	x1 := int(math.Ceil(centre.X + half))
	y0 := int(math.Floor(centre.Y - half))
	y1 := int(math.Ceil(centre.Y + half))
	if x1 < 0 || y1 < 0 || x0 >= w || y0 >= h {
		return
	}
	x0, y0 = maxInt(0, x0), maxInt(0, y0)
	x1, y1 = minInt(w, x1), minInt(h, y1)
	tex := textures[sp.Texture]
	// Additive sprites brighten what is under them; the rest are laid over
	// it. Both write into a transparent picture, which is then painted over
	// the frame, so the frame shows through wherever nothing was drawn.
	alpha := clampFloat64(sp.A, 0, 1)
	if alpha <= 0 {
		return
	}
	cr := clampFloat64(sp.R, 0, 1) * alpha
	cg := clampFloat64(sp.G, 0, 1) * alpha
	cb := clampFloat64(sp.B, 0, 1) * alpha
	span := half * 2
	for y := y0; y < y1; y++ {
		v := (float64(y) + 0.5 - (centre.Y - half)) / span
		for x := x0; x < x1; x++ {
			u := (float64(x) + 0.5 - (centre.X - half)) / span
			var r, g, b, a float64 = cr, cg, cb, alpha
			if tex != nil {
				tr, tg, tb, ta := sampleFxTexture(tex, u, v)
				if ta > 0 {
					// The map carries the shape; the node's colour
					// ramp carries what it is doing to it.
					r, g, b = r*tr*ta, g*tg*ta, b*tb*ta
					a *= ta
				}
			}
			if a <= 0 {
				continue
			}
			blendFxPixel(dst, x, y, r, g, b, a, sp.Additive)
		}
	}
}

// fxProject turns a point in model space into a pixel, reporting false when it
// is behind the camera or so far off that the sprite cannot be on screen.
func fxProject(view fauxgl.Matrix, p fauxgl.Vector, w, h int) *fauxgl.Vector {
	return engine.Project(view, p, w, h)
}

// sampleFxTexture reads one texel of a sprite's map. The rows are taken as
// they are stored: the game keeps its maps top down, and a sprite map is read
// the same way whether it came off disk or out of a .rsh.
func sampleFxTexture(tex *image.RGBA, u, v float64) (r, g, b, a float64) {
	rect := tex.Bounds()
	x := rect.Min.X + int(u*float64(rect.Dx()))
	y := rect.Min.Y + int(v*float64(rect.Dy()))
	if x < rect.Min.X {
		x = rect.Min.X
	}
	if y < rect.Min.Y {
		y = rect.Min.Y
	}
	if x >= rect.Max.X {
		x = rect.Max.X - 1
	}
	if y >= rect.Max.Y {
		y = rect.Max.Y - 1
	}
	i := tex.PixOffset(x, y)
	return float64(tex.Pix[i]) / 255, float64(tex.Pix[i+1]) / 255,
		float64(tex.Pix[i+2]) / 255, float64(tex.Pix[i+3]) / 255
}

// blendFxPixel mixes one sprite pixel into the overlay. The picture is
// premultiplied, so what goes in is the colour already scaled by its alpha.
func blendFxPixel(dst *image.RGBA, x, y int, r, g, b, a float64, additive bool) {
	i := dst.PixOffset(x, y)
	if additive {
		// Brighten, and let the picture grow no brighter than the sprite
		// itself: without the cap a hundred overlapping particles pile up
		// into a white square.
		dst.Pix[i] = clampByte(float64(dst.Pix[i]) + r*255)
		dst.Pix[i+1] = clampByte(float64(dst.Pix[i+1]) + g*255)
		dst.Pix[i+2] = clampByte(float64(dst.Pix[i+2]) + b*255)
		dst.Pix[i+3] = clampByte(math.Max(float64(dst.Pix[i+3]), a*255))
		return
	}
	// Over: the sprite in front of what is already there.
	in := a * 255
	if in <= 0 {
		return
	}
	keep := 1 - a
	dst.Pix[i] = clampByte(r*255 + float64(dst.Pix[i])*keep)
	dst.Pix[i+1] = clampByte(g*255 + float64(dst.Pix[i+1])*keep)
	dst.Pix[i+2] = clampByte(b*255 + float64(dst.Pix[i+2])*keep)
	dst.Pix[i+3] = clampByte(in + float64(dst.Pix[i+3])*keep)
}

func clampByte(v float64) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 255 {
		return 255
	}
	return uint8(v + 0.5)
}

// fxTextureCandidates is where a sprite's Texture property can name its map
// from. The property is written every which way: a full data path, a path
// relative to the effects folder, or a bare file name — which is where the
// sprite maps of the base game live, since the FX editor wrote them next to
// the effect it was drawing.
func fxTextureCandidates(name string) []string {
	p := strings.ReplaceAll(name, `\`, "/")
	for _, prefix := range []string{"data:", "data/", "./"} {
		if len(p) >= len(prefix) && strings.EqualFold(p[:len(prefix)], prefix) {
			p = p[len(prefix):]
		}
	}
	p = strings.TrimLeft(p, "/")
	if p == "" {
		return nil
	}
	if !strings.Contains(p, ".") {
		p += ".tga"
	}
	if strings.HasPrefix(strings.ToLower(p), "art/fx/") {
		return []string{p}
	}
	return []string{"art/fx/" + p, p}
}

// fxTexturePath is the name a sprite's map is filed under in the overlay,
// which is the path relative to the effects folder: what the map is called
// rather than where it was looked for.
func fxTexturePath(name string) string {
	c := fxTextureCandidates(name)
	if len(c) == 0 {
		return ""
	}
	return c[0]
}

// fxLoadResult is what the worker that resolves a model's effects sends back.
type fxLoadResult struct {
	effects  []fxOverlayEffect
	textures map[string]*image.RGBA
	// sounds is every sample the worker decoded, keyed by its virtual path.
	// Decoding a .fda takes long enough that doing it on the frame that first
	// plays one would drop the sound that asked for it.
	//
	// It is a cache rather than a map because the fills that decode a sound's
	// other variants run at the same time as each other and the caller reads
	// this as soon as the worker is done. SoundSet is how a caller gets at it.
	sounds *fx.SoundCache
	// soundBundles is the event name to the files that may serve it: a sound
	// path in a .whe names a folder of variants, and which of them plays is
	// the game's choice at the moment it plays.
	soundBundles map[string]fx.SoundBundle
	// timelines are the motion timelines of the .whe, which say when each of
	// those events fires in the clip. They came out of the same parse, so they
	// are carried rather than read again.
	timelines []whe.FxTimeline
	err       error
}

// fxTimelineMap indexes a model's motion timelines by event: event name (lower
// case) to the animations that fire it. A timeline names the clips it blends
// rather than always matching its own chunk name — a damage timeline drives
// the clip called vis_empty — so both are used as the animation to match.
func fxTimelineMap(timelines []whe.FxTimeline) map[string][]string {
	m, times := fxTimelineFires(timelines)
	out := make(map[string][]string, len(m))
	for ev, fires := range m {
		anims := make([]string, 0, len(fires))
		for _, f := range fires {
			anims = append(anims, f.Anim)
		}
		out[ev] = anims
	}
	_ = times
	return out
}

// fxEventFire is one timeline's instruction for an event: the clip it plays on,
// and the time in seconds into that clip where it starts.
type fxEventFire struct {
	Anim string
	// Time is seconds into the clip, with -1 meaning on entering it and the
	// largest float meaning on leaving.
	Time float64
}

// fxTimelineFires indexes a model's motion timelines by event, keeping the
// moment each event fires as well as the clip it belongs to. The time is the
// whole point: without it an effect has no start other than the start of the
// clip, which is what made every effect of a landing sequence go off together
// the moment the clip began.
func fxTimelineFires(timelines []whe.FxTimeline, clipDurations ...map[string]float64) (map[string][]fxEventFire, map[string]float64) {
	m := map[string][]fxEventFire{}
	add := func(ev, anim string, at float64) {
		if ev == "" || anim == "" {
			return
		}
		key := strings.ToLower(ev)
		for _, have := range m[key] {
			if strings.EqualFold(have.Anim, anim) && math.Abs(have.Time-at) < 1e-9 {
				return
			}
		}
		duration := 1.0
		if len(clipDurations) > 0 {
			if d := clipDurations[0][strings.ToLower(anim)]; d > 0 {
				duration = d
			}
		}
		at = whe.TimeNormalized(at, duration)
		m[key] = append(m[key], fxEventFire{Anim: anim, Time: at})
	}
	for _, tl := range timelines {
		anims := append([]string{tl.Name}, tl.Animations...)
		seenAnims := map[string]bool{}
		for _, a := range anims {
			key := strings.ToLower(a)
			if a == "" || seenAnims[key] {
				continue
			}
			seenAnims[key] = true
			for _, ev := range tl.Events {
				add(ev.Name, a, ev.Time)
			}
		}
	}
	// The earliest moment wins when a clip is named by two timelines, which is
	// what happens to a state a model enters rather than loops: the first pass
	// through the clip is the one where its entry events have already fired.
	times := map[string]float64{}
	for ev, fires := range m {
		for _, f := range fires {
			key := ev + "\x00" + strings.ToLower(f.Anim)
			if cur, seen := times[key]; !seen || f.Time < cur {
				times[key] = f.Time
			}
		}
	}
	return m, times
}

// fxTimelineFireTimes turns the fires index into the per-effect form
// fxOverlayEffect keeps: clip name to the moment the event starts on it.
func fxTimelineFireTimes(timelines []whe.FxTimeline, clipDurations ...map[string]float64) map[string]map[string]float64 {
	_, times := fxTimelineFires(timelines, clipDurations...)
	out := map[string]map[string]float64{}
	for key, at := range times {
		i := strings.LastIndex(key, "\x00")
		if i < 0 {
			continue
		}
		ev, anim := key[:i], key[i+1:]
		if out[ev] == nil {
			out[ev] = map[string]float64{}
		}
		out[ev][anim] = at
	}
	return out
}

// loadModelEffects resolves a model's events, parses every effect they name
// and decodes the sprite maps they use. It is meant to run on a worker
// goroutine: resolving a blueprint can mean a walk of the whole mod, which is
// far too slow to do while the window is waiting for a frame.
//
// A model with no events is not an error worth reporting — most of them have
// none — but an event that names a file which cannot be read is logged and
// skipped, so one broken effect does not cost the reader the other eleven.
// loadModelEffects is loadModelEffectsWhe with no live document: the .whe on
// disk is the whole story, which is every caller except an Events edit.
func loadModelEffects(tree *modtree.ModTree, model *WhmModel, modelFile string, decoded map[string]*fx.SoundSample) fxLoadResult {
	return loadModelEffectsWhe(tree, model, modelFile, decoded, nil)
}

// loadModelEffectsWhe resolves the model's effects with the live .whe
// document in hand when there is one: its definitions and its timelines are
// what the effects are built from, so values edited but not yet saved are the
// ones the preview shows.
func loadModelEffectsWhe(tree *modtree.ModTree, model *WhmModel, modelFile string, decoded map[string]*fx.SoundSample, liveWhe *whe.Document) fxLoadResult {
	res := fxLoadResult{
		textures:     map[string]*image.RGBA{},
		sounds:       fx.NewSoundCache(),
		soundBundles: map[string]fx.SoundBundle{},
	}
	// Samples the sound-only worker already decoded are handed over rather
	// than decoded again. Both workers read the same .whe, so without this the
	// effect of the FX button would be to pay for every sample of the model a
	// second time.
	if len(decoded) > 0 {
		for k, s := range decoded {
			res.sounds.Put(k, s)
		}
	}
	if tree == nil || model == nil {
		return res
	}
	bindings, _, err := fx.ResolveEventsForModel(tree, modelFile)
	if err != nil {
		res.err = err
		return res
	}
	if liveWhe != nil {
		bindings = fx.WheBindingsOverlay(bindings, liveWhe)
	}
	roots := materialSearchRoots(tree)
	seen := map[string]*image.RGBA{}
	// The .whe's timelines say which events belong to which animation, so a
	// model that has them only plays the ones its clip on screen fires. A
	// model whose effects all live in its blueprint has none, and then every
	// event it named plays at once, which is how this worked before.
	// Read and parse the .whe once and index it twice: which clip fires each
	// event, and when in that clip it fires. Reading it twice would parse a
	// 40 KB file of chunks a second time for an answer already in hand.
	var timelines []whe.FxTimeline
	if liveWhe != nil {
		timelines = liveWhe.Timelines()
	}
	if len(timelines) == 0 {
		// No document, or one whose parse produced no timelines: the file's
		// own copy is the fallback rather than an empty set, because empty
		// timelines mean "every event plays at once".
		timelines = fx.ReadWheTimelines(tree, modelFile)
	}
	durations := make(map[string]float64, len(model.Animations))
	for i := range model.Animations {
		a := &model.Animations[i]
		duration := float64(a.Duration)
		if duration <= 0 && a.NumFrames > 0 {
			duration = float64(a.NumFrames) / animFramesPerSecond(a)
		}
		if duration > 0 {
			durations[strings.ToLower(a.Name)] = duration
		}
	}
	fires, _ := fxTimelineFires(timelines, durations)
	tlAnims := map[string][]string{}
	for ev, list := range fires {
		anims := make([]string, 0, len(list))
		for _, f := range list {
			anims = append(anims, f.Anim)
		}
		tlAnims[ev] = anims
	}
	tlTimes := fxTimelineFireTimes(timelines, durations)
	timed := len(tlAnims) > 0
	for _, b := range bindings {
		fxPath, ok := b.Event.First("fx")
		if !ok || fxPath == "" {
			continue
		}
		data, err := readFxLuaFile(tree, roots, fxPath)
		if err != nil {
			log.Printf("3D preview: effect %q: %v", fxPath, err)
			continue
		}
		file, err := fx.ParseFxLua(data)
		if err != nil {
			log.Printf("3D preview: effect %q: %v", fxPath, err)
			continue
		}
		sim := fx.NewFxSim(file)
		marker := fxEventMarker(model, b.Event, len(res.effects))
		other := fxBeamOtherMarker(model, b.Event)
		e := fxOverlayEffect{
			Sim:         sim,
			Label:       b.Event.Name,
			FxName:      fxPath,
			Marker:      marker,
			HasOther:    other >= 0,
			OtherMarker: other,
			Spans:       fxEventSpansDistance(b.Event),
			// An event no clip fires is not one the preview plays on its
			// own: it waits for whatever triggers it in the game, which is
			// a hit landing or a unit dying rather than the model standing
			// there. Only a model with no timelines plays everything.
			Timed:      timed,
			Animations: tlAnims[strings.ToLower(b.Event.Name)],
			FireTimes:  tlTimes[strings.ToLower(b.Event.Name)],
		}
		// fx_scale applies uniformly to this event's local effect geometry
		// on top of the viewer's own scale; the marker origin itself stays put.
		if s, ok := b.Event.First("fx_scale"); ok {
			if f, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil && f > 0 {
				e.EventScale = f
			}
		}
		rest := model.RestPose()
		if marker >= 0 {
			if world, ok := model.MarkerWorld(marker, rest, true); ok {
				e.Origin = world
			}
		} else {
			// No marker, or one this model does not carry: the middle
			// of the model is where an effect plays by default.
			e.Origin, _ = model.ModelCenter()
		}
		if e.HasOther {
			if world, ok := model.MarkerWorld(other, rest, true); ok {
				e.Far = world
			}
		}
		if axis, ok := model.MarkerAxis(marker, rest, true); ok {
			e.Axis = axis
		}
		if right, up, ok := model.MarkerBasis(marker, rest, true); ok {
			e.Right = right
			e.Up = up
		}
		res.effects = append(res.effects, e)
		// Every map the effect names, decoded once however many nodes
		// point at it. The sprites are asked for rather than the file
		// scanned, so only the maps the running effect actually uses
		// are paid for.
		for _, sprite := range sim.Sprites() {
			path := fxTexturePath(sprite.Texture)
			if path == "" {
				continue
			}
			if _, done := seen[path]; done {
				continue
			}
			img, err := decodeFxTexture(tree, roots, path)
			if err != nil {
				log.Printf("3D preview: sprite texture %q: %v", path, err)
				seen[path] = nil
				continue
			}
			seen[path] = img
		}
		// A beam's map is named by the node, not by anything on show, so it
		// is read straight off the parsed file.
		// A beam's and a ring's map are named by the node, not by anything
		// on show, so they are read straight off the parsed file. Both are
		// asked with a unit span, which is all the sampler needs to name
		// the file they will draw from.
		want := map[string]bool{}
		for _, beam := range sim.Beams(fauxgl.Vector{}, fauxgl.Vector{X: 1}, false) {
			want[fxTexturePath(beam.Texture)] = true
		}
		for _, ring := range sim.Rings(fauxgl.Vector{}, fauxgl.Vector{X: 1},
			fauxgl.Vector{Y: 1}, fauxgl.Vector{}, fauxgl.Vector{Z: 1}) {
			want[fxTexturePath(ring.Texture)] = true
		}
		for path := range want {
			if path == "" {
				continue
			}
			if _, done := seen[path]; done {
				continue
			}
			img, err := decodeFxTexture(tree, roots, path)
			if err != nil {
				log.Printf("3D preview: effect texture %q: %v", path, err)
				seen[path] = nil
				continue
			}
			seen[path] = img
		}
	}
	for path, img := range seen {
		if img != nil {
			res.textures[path] = img
		}
	}
	fxLoadWheAudio(tree, modelFile, liveWhe, &res)
	return res
}

// fxLoadWheAudio fills res with the .whe's declared sounds: from the live
// document when one is being edited, from the tree's copy of the file
// otherwise.
func fxLoadWheAudio(tree *modtree.ModTree, modelFile string, liveWhe *whe.Document, res *fxLoadResult) {
	if liveWhe != nil {
		if data, err := liveWhe.MarshalBinary(); err == nil && len(data) > 0 {
			loadWheSoundsFrom(tree, data, liveWhe.Timelines(), res)
			return
		}
	}
	loadWheSounds(tree, modelFile, res)
}

// loadModelSounds resolves and decodes a model's declared sounds on their own,
// without touching its effects. It is the sound half of loadModelEffects,
// split out so a model on screen has its audio ready without the FX toggle
// having been pressed and without resolving a blueprint walk it may never
// need.
// loadModelSounds is loadModelSoundsWhe with no live document, which is
// every caller except an Events edit.
func loadModelSounds(tree *modtree.ModTree, modelFile string) fxLoadResult {
	return loadModelSoundsWhe(tree, modelFile, nil, nil)
}

// loadModelSoundsWhe resolves and decodes the declared sounds, from the live
// .whe document when one is being edited so an unsaved sound path is what
// gets decoded. decoded is what the outgoing player already holds: those
// samples are seeded into the new cache rather than decoded again, so a
// reload only pays for the paths that changed.
func loadModelSoundsWhe(tree *modtree.ModTree, modelFile string, liveWhe *whe.Document, decoded map[string]*fx.SoundSample) fxLoadResult {
	res := fxLoadResult{
		sounds:       fx.NewSoundCache(),
		soundBundles: map[string]fx.SoundBundle{},
	}
	if tree == nil {
		return res
	}
	if len(decoded) > 0 {
		for k, s := range decoded {
			res.sounds.Put(k, s)
		}
	}
	fxLoadWheAudio(tree, modelFile, liveWhe, &res)
	return res
}

// loadWheSounds reads the sound events out of a .whe and decodes every sample
// they name, so the frame that plays one only has to hand the samples over.
//
// Only the variants of the sounds the model's own timelines actually fire are
// decoded. A .whe declares sounds for states the preview never plays — the ones
// a shot impact or a death would trigger — and decoding an .fda costs far more
// than drawing it, so the rest are resolved and left on disk.
func loadWheSounds(tree *modtree.ModTree, modelFile string, res *fxLoadResult) {
	data := readFxWheFile(tree, modelFile)
	if len(data) == 0 {
		return
	}
	loadWheSoundsFrom(tree, data, fx.ReadWheTimelines(tree, modelFile), res)
}

// loadWheSoundsFrom is loadWheSounds with the .whe bytes and the timelines
// handed in: the live document being edited marshals its own, so an unsaved
// sound edit is what gets decoded and wired.
func loadWheSoundsFrom(tree *modtree.ModTree, data []byte, timelines []whe.FxTimeline, res *fxLoadResult) {
	// One worker per sound that names a folder of variants, all of them
	// finished before this returns: the samples are handed to the player as one
	// set once it does.
	var fills sync.WaitGroup
	declared := whe.ParseWheSounds(data)
	if len(declared) == 0 {
		return
	}
	// Which events the timelines on screen can fire at all, so the samples for
	// the rest are never decoded.
	res.timelines = timelines
	played := map[string]bool{}
	for _, tl := range timelines {
		for _, ev := range tl.Events {
			played[strings.ToLower(ev.Name)] = true
		}
	}
	roots := materialSearchRoots(tree)
	// Declared but never fired is normal rather than a failure: the sound
	// belongs to a state this model is not in.
	for name, ev := range declared {
		if ev.Path == "" || len(played) == 0 && !fxSoundAlwaysPlays(name) {
			continue
		}
		if len(played) > 0 && !played[name] {
			continue
		}
		b, ok := fx.ResolveSound(tree, roots, ev.Path)
		if !ok {
			log.Printf("3D preview: sound %q: %v", ev.Path, whe.SoundMissing(ev.Path))
			continue
		}
		// The samples every variant needs are decoded now: the first of a
		// folder inline and the rest by the fills below, so that nothing is
		// still being decoded by the time this hands its result over.
		first := b.Files[0]
		if _, done := res.sounds.Get(first); !done {
			s, err := fxSoundDecodeForPlayback(tree, roots, first)
			if err != nil {
				log.Printf("3D preview: sound %q: %v", ev.Path, err)
				continue
			}
			res.sounds.Put(first, s)
		}
		res.soundBundles[name] = b
		if len(b.Files) > 1 {
			fills.Add(1)
			go func(b fx.SoundBundle) {
				defer fills.Done()
				fxSoundFillVariants(tree, roots, b, res.sounds)
			}(b)
		}
	}
	// The fills have to be finished before the result is handed on. They write
	// into the same map the caller is about to read and then keep, and a map
	// written by one goroutine while another reads it is not a map with a
	// missing key in it but a crash. The first sample of each sound is already
	// decoded by now, so this waits for the variants of a footfall folder and
	// not for the model's audio as a whole.
	fills.Wait()
}

// fxSoundAlwaysPlays says whether a sound the model's timelines never fire is
// still worth resolving: none are. It exists so the one place that decides has
// a name to read rather than a bare false.
func fxSoundAlwaysPlays(name string) bool { return false }

// fxSoundFillVariants decodes the rest of a sound's samples alongside the
// others, so a footfall folder of seven files is ready by the time the second
// step lands rather than one step at a time.
//
// Every fill for a model runs at once rather than one after another: the files
// are independent, they are decoded off the frame that asked for them, and a
// model with three sound folders would otherwise decode them in sequence for
// no gain. They write into one cache, so it is a fx.SoundCache rather than a
// bare map and the caller's Wait is what makes handing it over safe.
func fxSoundFillVariants(tree *modtree.ModTree, roots []string, b fx.SoundBundle, into *fx.SoundCache) {
	for _, f := range b.Files[1:] {
		s, err := fxSoundDecodeForPlayback(tree, roots, f)
		if err != nil {
			continue
		}
		into.Put(f, s)
	}
}

// fxSoundDeclaredRate returns the rate a decoded sample should be played at.
//
// This is not simply the rate the file declares, and the reason is worth
// stating because it was measured rather than assumed. The Relic decoder emits
// the same samples whatever rate it is handed: decoding land (1).fda at 22050,
// 32000, 44100 and 48000 produces byte-identical PCM (776704 frames every
// time). A sample rate is therefore not in the audio at all, it is only a label
// on it, and there is no spectral test that can recover it. Passing 22050 and
// 44100 to the same decoder cannot be told apart by any amount of analysis,
// because they are the same samples.
//
// What settles it instead is the block bitrate. Every .fda in the game carries
// BlockBitrate 2048, which is the frame length in samples, and the number of
// decoded frames is fixed by the file's size. The rate is whatever the encoder
// used, and land (1).fda was authored at 44100 while its INFO says 22050: the
// same samples, read at 22050, last twice as long and sound an octave down.
//
// So the declared rate is corrected here rather than measured. 22050 is the
// rate that is wrong, and 44100 is what such files really are; a file that
// declares anything else is taken at its word. The two values are named so a
// test can state what it expects without repeating the story.
const (
	fxSoundBogusRate = 22050
	fxSoundRealRate  = 44100
)

func fxSoundDeclaredRate(declared int) int {
	if declared == fxSoundBogusRate {
		return fxSoundRealRate
	}
	return declared
}

// fxSoundDecodeForPlayback decodes one sample and converts it into the audio
// context's format straight away.
//
// The conversion belongs here rather than at the moment a sound plays, and the
// reason is measured rather than stylistic: a windowed sinc conversion of a
// second of audio costs about 28 ms, against 2.4 ms for the linear
// interpolation it replaced. On the frame that starts a sound that is a visible
// hitch, and a walk cycle starts a sound on every step, so it would be paid
// over and over for a conversion whose answer never changes.
//
// Doing it on the worker instead means the frame only ever hands finished PCM to
// the player, and the same sample converted once serves every step that uses it.
func fxSoundDecodeForPlayback(tree *modtree.ModTree, roots []string, p string) (*fx.SoundSample, error) {
	info, err := fx.SoundRead(tree, roots, p)
	if err != nil {
		return nil, err
	}
	pcm := wavPCM16(info.PCMData)
	if real := fxSoundDeclaredRate(info.SampleRate); real != info.SampleRate {
		// Reading the sample at its real rate rather than the declared one is
		// what makes it the right length and the right pitch; the samples
		// themselves are untouched.
		log.Printf("3D preview: sound %s declares %d Hz, played at %d Hz",
			filepath.Base(p), info.SampleRate, real)
		info = &wav.Info{SampleRate: real, Channels: info.Channels, PCMData: info.PCMData}
	}
	// The context is created once for the process and its format is fixed, so
	// the target is known here without asking it.
	if sr, ch := info.SampleRate, info.Channels; sr != audioplayer.SampleRate || ch != audioplayer.Channels {
		converted, err := fda.ResamplePCM16(pcm, sr, ch, audioplayer.SampleRate, audioplayer.Channels)
		if err != nil {
			return nil, fmt.Errorf("convert %s: %w", p, err)
		}
		pcm = converted
		info = &wav.Info{SampleRate: audioplayer.SampleRate, Channels: audioplayer.Channels, PCMData: fda.PCM16Bytes(pcm)}
	}
	return &fx.SoundSample{Info: info, PCM: pcm}, nil
}

// readFxWheFile reads the .whe beside a model, which is where both its effects
// and its sounds are declared.
func readFxWheFile(tree *modtree.ModTree, modelFile string) []byte {
	return fx.ReadWheData(tree, modelFile)
}

// fxBeamOtherMarker is the second marker of a beam event, -1 when the event
// names none. This is the entry that makes a STYLE_BEAM drawable at all: the
// ribbon spans from the event's own marker to this one, and
// fx_length = distance says to stretch it over exactly what lies between
// them rather than over the Length curve in model units.
//
//	fx_other_marker = marker_gun_muzzle_ctr_out
//	fx_other        = self            (or "target": the shot's other end)
//	fx_length       = distance
//	fx_transform    = self_to_other
//
// fx_other = target names no marker of the model's own — the far end is the
// unit that was shot, which a model viewer has no way of knowing — so those
// are left for the caller to supply and resolve to -1 here.
func fxBeamOtherMarker(model *WhmModel, ev whe.FxEvent) int {
	if other, ok := ev.First("fx_other"); ok && !strings.EqualFold(other, "self") {
		return -1
	}
	for _, k := range []string{"fx_other_marker", "fx_other_marker_0"} {
		name, ok := ev.First(k)
		if !ok || name == "" || strings.EqualFold(name, "random") {
			continue
		}
		if i := model.MarkerIndex(name); i >= 0 {
			return i
		}
	}
	return -1
}

// fxEventBeamLengths reports whether the event asked for the beam to span the
// distance between its markers. Anything else leaves the Length curve read as
// model units, which is how an effect with no partner marker draws.
func fxEventSpansDistance(ev whe.FxEvent) bool {
	v, ok := ev.First("fx_length")
	return ok && strings.EqualFold(strings.TrimSpace(v), "distance")
}

// fxEventMarker is the marker of the model an event plays at, or -1 when it
// names none the model carries. An event may name it outright, through one of
// the numbered keys the game uses when a place is picked out of a set, or
// ask for one of the model's own at random — which is what "marker = random"
// means, and what a model without markers of its own ends up playing at the
// middle of.
func fxEventMarker(model *WhmModel, ev whe.FxEvent, eventIndex int) int {
	keys := []string{"marker", "marker_0", "marker_surface_00", "marker_surface_0", "random_marker_0"}
	for _, k := range keys {
		name, ok := ev.First(k)
		if !ok || name == "" {
			continue
		}
		if strings.EqualFold(name, "random") {
			// One of the model's own markers, chosen the same way
			// every run so a preview does not shuffle itself.
			if len(model.Markers) > 0 {
				return (eventIndex*7 + 3) % len(model.Markers)
			}
			return -1
		}
		if i := model.MarkerIndex(name); i >= 0 {
			return i
		}
	}
	return -1
}

// fxLuaPath is the file an event's "fx" entry names. The game writes it as a
// data path with or without its prefix, and the effect lives under art/fx.
func fxLuaPath(name string) string {
	p := strings.ReplaceAll(name, `\`, "/")
	for _, prefix := range []string{"data:", "data/"} {
		if len(p) >= len(prefix) && strings.EqualFold(p[:len(prefix)], prefix) {
			p = p[len(prefix):]
		}
	}
	p = strings.TrimLeft(p, "/")
	if p == "" {
		return ""
	}
	if !strings.Contains(strings.ToLower(p), "art/fx/") {
		p = "art/fx/" + p
	}
	if !strings.Contains(strings.ToLower(p), ".lua") {
		p += ".lua"
	}
	return p
}

// readFxLuaFile reads an effect file from the tree or from the data folders on
// disk, the same two places a texture is looked for in.
func readFxLuaFile(tree *modtree.ModTree, roots []string, name string) ([]byte, error) {
	p := fxLuaPath(name)
	if p == "" {
		return nil, fmt.Errorf("no effect file named")
	}
	if data, err := tree.ReadFile(p); err == nil && len(data) > 0 {
		return data, nil
	}
	if f := tree.FindByName(strings.ToLower(path.Base(p))); f != nil {
		if data, err := tree.ReadTreeFile(f); err == nil {
			return data, nil
		}
	}
	rel := filepath.FromSlash(p)
	lower := filepath.FromSlash(strings.ToLower(p))
	for _, root := range roots {
		for _, name := range []string{rel, lower} {
			if data, err := os.ReadFile(filepath.Join(root, name)); err == nil && len(data) > 0 {
				return data, nil
			}
		}
	}
	return nil, fmt.Errorf("effect file %s not found", p)
}

// decodeFxTexture decodes a sprite map. Effects keep theirs as .tga or .dds
// under art/fx, which the image decoder already reads; what is missing is a
// plain RGBA copy, since that is what the overlay samples.
func decodeFxTexture(tree *modtree.ModTree, roots []string, name string) (*image.RGBA, error) {
	candidates := fxTextureCandidates(name)
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no texture named")
	}
	var lastErr error
	for _, p := range candidates {
		data, format, err := readFxTextureBytes(tree, roots, p)
		if err != nil {
			lastErr = err
			continue
		}
		img, _, err := decodeImageData(data, format)
		if err != nil {
			return nil, err
		}
		return toRGBA(img), nil
	}
	return nil, lastErr
}

// readFxTextureBytes finds a sprite map and reports the format to decode it
// as, which is the extension of the file that was actually read.
//
// The property names its map in whatever case the FX editor was saving in —
// ART/FX/RALLY_07.TGA — while the files on disk are usually all lower case, so
// a direct hit on Windows alone is not enough: every folder of the path is
// also tried lower cased. Without that the game's own sprite maps read as
// missing and the effects draw as bare coloured discs.
//
// The extension is not always right either. The FX editor wrote .tga into
// every one of its files, while the maps that shipped with the game were
// converted later and are filed as .dds — art/fx/AURA_FX/TEXTURES/NEG.TGA is
// art/fx/aura_fx/textures/neg.dds, and the same for most of the effect maps
// the base game owns. A name is therefore also tried with the other map
// extension, and by its stem alone, since a file's name without its extension
// is what identifies it across both.
func readFxTextureBytes(tree *modtree.ModTree, roots []string, p string) ([]byte, string, error) {
	format := strings.TrimPrefix(strings.ToLower(path.Ext(p)), ".")
	for _, cand := range fxTextureTries(p) {
		ext := strings.TrimPrefix(strings.ToLower(path.Ext(cand)), ".")
		if data, err := tree.ReadFile(cand); err == nil && len(data) > 0 {
			return data, ext, nil
		}
		if data, err := readFxTextureByName(tree, cand); err == nil {
			return data, ext, nil
		}
		for _, root := range roots {
			rel := filepath.FromSlash(cand)
			lower := filepath.FromSlash(strings.ToLower(cand))
			for _, name := range []string{rel, lower} {
				if data, err := os.ReadFile(filepath.Join(root, name)); err == nil && len(data) > 0 {
					return data, ext, nil
				}
			}
		}
	}
	return nil, format, fmt.Errorf("sprite texture %s not found", p)
}

// readFxTextureByName looks a map up by its bare file name, which is how a
// model or a texture reference names one it keeps beside itself. The stem is
// tried as well as the name, so neg.tga finds neg.dds.
func readFxTextureByName(tree *modtree.ModTree, p string) ([]byte, error) {
	base := strings.ToLower(path.Base(p))
	for _, name := range []string{base, strings.TrimSuffix(base, path.Ext(base))} {
		if name == "" {
			continue
		}
		if f := tree.FindByName(name); f != nil {
			if data, err := tree.ReadTreeFile(f); err == nil && len(data) > 0 {
				return data, nil
			}
		}
	}
	return nil, fmt.Errorf("sprite texture %s not found by name", p)
}

// fxTextureTries lists the paths a map is looked for under, best first: the
// name as written, then the same name in the other extension the game's maps
// come in, which is what makes a .tga reference reach a .dds file.
func fxTextureTries(p string) []string {
	out := []string{p}
	stem := strings.TrimSuffix(p, path.Ext(p))
	ext := strings.ToLower(path.Ext(p))
	// .rtx is what the game actually loads, and the shipped art/fx maps named
	// by an effect file exist under no other extension: the editor writes a
	// .dds name into the effect and the archives hold the compiled .rtx.
	//
	// It is also tried before .dds because the two are not the same map.
	// Across the 2002 shipped FX maps, 453 carry no alpha at all, and the
	// .dds of a name is far likelier to be one of them than the .rtx of the
	// same name: ah_lascannon_image, neg and attack_move all decode
	// opaque from .dds and translucent from the .rtx beside them. Preferring
	// .dds therefore draws a map whose black background is opaque, which is a
	// black rectangle sitting over the model where a glow should be. A loose
	// file the effect names exactly is still tried first, and .dds remains in
	// the list for the maps that ship as nothing but.
	for _, alt := range []string{".rtx", ".dds", ".tga"} {
		if ext == alt {
			continue
		}
		out = append(out, stem+alt)
	}
	return out
}

// toRGBA copies any decoded picture into the RGBA layout the overlay samples.
func toRGBA(src image.Image) *image.RGBA {
	if out, ok := src.(*image.RGBA); ok {
		return out
	}
	b := src.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(out, out.Bounds(), src, b.Min, draw.Src)
	return out
}

// drawFxBeam fills one ribbon of a beam into the overlay. The quads are the
// same ones the Vulkan path hands the rasteriser, walked here one pixel at a
// time: the software renderer has no vertex stage, so the strip is projected
// here and then sampled with the map the same way a sprite's square is.
func drawFxBeam(dst *image.RGBA, view fauxgl.Matrix, eye fauxgl.Vector, w, h int, rb fx.BeamRibbon, textures map[string]*image.RGBA) {
	verts := fx.BeamQuads(rb, eye)
	if len(verts) < 4 {
		return
	}
	tex := textures[fxTexturePath(rb.Texture)]
	alpha := clampFloat64(rb.Alpha, 0, 1)
	if alpha <= 0 {
		return
	}
	cr := clampFloat64(rb.R, 0, 1) * alpha
	cg := clampFloat64(rb.G, 0, 1) * alpha
	cb := clampFloat64(rb.B, 0, 1) * alpha
	// The screen positions of every corner, and which ones were off screen.
	pts := make([]*fauxgl.Vector, len(verts))
	any := false
	for i := range verts {
		pts[i] = fxProject(view, verts[i].Pos, w, h)
		if pts[i] != nil {
			any = true
		}
	}
	if !any {
		return
	}
	n := len(verts) / 2
	for i := 0; i+1 < n; i++ {
		a, b := i*2, (i+1)*2
		drawFxBeamQuad(dst, pts[a], pts[b], pts[a+1], pts[b+1],
			verts[a], verts[b], verts[a+1], verts[b+1],
			tex, cr, cg, cb, alpha, rb.Additive, w, h)
	}
}

// drawFxBeamQuad fills the quad between two cross-sections of a ribbon. A
// cross-section that fell off screen has no pixel, so the quad is skipped
// rather than smeared — the beam reappears once the camera comes back round.
func drawFxBeamQuad(dst *image.RGBA, p0, p1, q0, q1 *fauxgl.Vector, v0, v1, v2, v3 fx.BeamVert,
	tex *image.RGBA, cr, cg, cb, alpha float64, additive bool, w, h int) {
	if p0 == nil || p1 == nil || q0 == nil || q1 == nil {
		return
	}
	x0 := math.Floor(math.Min(math.Min(p0.X, p1.X), math.Min(q0.X, q1.X)))
	x1 := math.Ceil(math.Max(math.Max(p0.X, p1.X), math.Max(q0.X, q1.X)))
	y0 := math.Floor(math.Min(math.Min(p0.Y, p1.Y), math.Min(q0.Y, q1.Y)))
	y1 := math.Ceil(math.Max(math.Max(p0.Y, p1.Y), math.Max(q0.Y, q1.Y)))
	ix0, iy0 := maxInt(0, int(x0)), maxInt(0, int(y0))
	ix1, iy1 := minInt(w, int(x1)), minInt(h, int(y1))
	if ix0 >= ix1 || iy0 >= iy1 {
		return
	}
	// The two triangles of the quad, each with the map coordinate at each of
	// its three corners, so the texture is interpolated across it the way the
	// GPU's would be.
	tris := [2][3]struct {
		p *fauxgl.Vector
		v fx.BeamVert
	}{
		{{p0, v0}, {p1, v1}, {q1, v3}},
		{{p0, v0}, {q1, v3}, {q0, v2}},
	}
	den := func(a, b, c *fauxgl.Vector) float64 {
		return (b.Y-c.Y)*(a.X-c.X) + (c.X-b.X)*(a.Y-c.Y)
	}
	for _, tri := range tris {
		d := den(tri[0].p, tri[1].p, tri[2].p)
		if math.Abs(d) < 1e-9 {
			continue
		}
		for y := iy0; y < iy1; y++ {
			py := float64(y) + 0.5
			for x := ix0; x < ix1; x++ {
				px := float64(x) + 0.5
				l0 := ((tri[1].p.Y-tri[2].p.Y)*(px-tri[2].p.X) + (tri[2].p.X-tri[1].p.X)*(py-tri[2].p.Y)) / d
				l1 := ((tri[2].p.Y-tri[0].p.Y)*(px-tri[2].p.X) + (tri[0].p.X-tri[2].p.X)*(py-tri[2].p.Y)) / d
				l2 := 1 - l0 - l1
				if l0 < 0 || l1 < 0 || l2 < 0 {
					continue
				}
				u := l0*tri[0].v.U + l1*tri[1].v.U + l2*tri[2].v.U
				v := l0*tri[0].v.V + l1*tri[1].v.V + l2*tri[2].v.V
				r, g, b, a := cr, cg, cb, alpha
				if tex != nil {
					// The texel's alpha is the quad's alpha, empty
					// space and all. A map that is transparent round
					// its subject has to leave that subject alone:
					// the necron crystal's panels are 78% empty and
					// the shape of what is left is the panel, so
					// filling the empty space with the flat colour
					// draws a solid square where a tapered facet
					// belongs. A map with no alpha of its own decodes
					// fully opaque and is unaffected.
					tr, tg, tb, ta := sampleFxTexture(tex, u, v)
					r, g, b, a = r*tr*ta, g*tg*ta, b*tb*ta, alpha*ta
				}
				if a <= 0 {
					continue
				}
				blendFxPixel(dst, x, y, r, g, b, a, additive)
			}
		}
	}
}
