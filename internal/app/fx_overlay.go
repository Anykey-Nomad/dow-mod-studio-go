package app

import (
	"dow-mod-studio/internal/app/uikit"
	"dow-mod-studio/internal/app/uistate"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"log"
	"math"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"dow-mod-studio/internal/3d_engine"
	"dow-mod-studio/internal/app/fxscene"
	"dow-mod-studio/internal/app/render"
	"dow-mod-studio/internal/audioplayer"
	"dow-mod-studio/internal/fda"
	"dow-mod-studio/internal/fx"
	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/wav"
	"dow-mod-studio/internal/whe"
	"dow-mod-studio/internal/whm"
	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget/material"
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

// The frame's particle scene and its light live in internal/app/fxscene, a
// leaf the overlay builds for and both renderers read from. The names below
// are aliases, so the builder, the GPU path and the tests keep the names they
// were written with.
type (
	fxGpuScene   = fxscene.Scene
	fxGpuGroup   = fxscene.Group
	fxGpuBeam    = fxscene.Beam
	fxSceneLight = fxscene.Light
)

// fxSceneLightOf moved to internal/app/render: it derives the frame light
// from the render request the GPU path is given.

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
			rb = e.FxScaleRibbon(rb, preservesMarkerSpan)
			if rb.Alpha <= 0 || rb.Width <= 0 {
				return
			}
			name := fxTexturePath(rb.Texture)
			// Beam vertices are in model space and need no shared origin, but
			// keep their groups separate from particle groups by effect site.
			g := group(fxGroupKey{additive: rb.Additive, texture: name, origin: fxGroupOrigin(e.Origin)}, e.Origin)
			g.Beams = append(g.Beams, fxGpuBeam{Ribbon: rb, Eye: eye})
		}
		spansMarkers := e.FxSpansMarkers()
		for _, rb := range e.Sim.Beams(e.Origin, e.FxFarPoint(), spansMarkers) {
			rb = e.FxMarkerRibbon(rb)
			if e.TurnAngles != [3]float64{} {
				rb.From = e.Origin.Add(e.FxTurnVector(rb.From.Sub(e.Origin)))
				rb.To = e.Origin.Add(e.FxTurnVector(rb.To.Sub(e.Origin)))
			}
			addRibbon(rb, spansMarkers)
		}
		axX, axY, axZ := e.FxRingFrame()
		markerFrame := [3]fauxgl.Vector{axX, axY, axZ}
		for _, rb := range e.Sim.Rings(e.Origin, axX, axY, axZ, camUp) {
			rb = e.FxTurnRibbon(rb)
			addRibbon(rb, false)
		}
		// Particles are composited after their beams. Their offsets and radii
		// use the same model-space scale as the CPU overlay path.
		for _, sp := range e.Sim.Sprites() {
			if sp.A <= 0 {
				continue
			}
			sp = e.FxTurnSprite(sp)
			sp = fxOrientSprite(sp, markerFrame)
			sp = fxScaleSprite(sp, e.FxScaleOrOne())
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
	case v.ShowFX && len(v.FxEffects) > 0:
		return fmt.Sprintf("FX %d", len(v.FxEffects))
	case v.ShowFX:
		return "FX…"
	case v.FxErr != "":
		return "FX !"
	}
	return "FX"
}

// modelFXScaleStep is how much one press of the scale buttons moves it, and
// the range the control is bounded to. FX dimensions are already interpreted
// in the model's coordinate units; this control is a uniform preview adjustment.
const (
	modelFXScaleStep = 1.25
	modelFXScaleMin  = 0.02
	modelFXScaleMax  = 4
)

// scaleFX multiplies the effect scale, keeping it inside its range. It is
// multiplicative so repeated presses walk the whole range evenly rather than
// bunching up at the ends.
func ScaleFX(v *modelViewer, f float64) {
	if v == nil || f <= 0 {
		return
	}
	s := v.FxScale
	if s <= 0 {
		s = fxScaleDefaultValue
	}
	s = engine.ClampFloat64(s*f, modelFXScaleMin, modelFXScaleMax)
	v.FxScale = s
	Invalidate(v)
	log.Printf("3D preview: FX scale %.2f", s)
}

// modelFXScaleLabel is what the scale reads on the button row: the value, so
// it can be read off a still frame while it is being dialled in.
func modelFXScaleLabel(v *modelViewer) string {
	if v == nil {
		return "FX x1.00"
	}
	s := v.FxScale
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
func ToggleFX(v *modelViewer) {
	if v == nil {
		return
	}
	if v.ShowFX {
		v.ShowFX = false
		v.FxOverlay = nil
		v.FxLast = time.Time{}
		log.Printf("3D preview: effects off")
		return
	}
	v.ShowFX = true
	v.FxOverlay = nil
	v.FxLast = time.Time{}
	log.Printf("3D preview: effects on (%d already loaded)", len(v.FxEffects))
}

// fxStartLoad begins resolving the model's effects, once, reporting whether it
// started anything. The path is the one the model itself was read from, which
// is how its blueprint is found.
func FxStartLoad(ui *uiState, v *modelViewer) bool {
	if v == nil || v.Model == nil || ui.ModTree == nil || v.ModelFile == "" {
		return false
	}
	if v.FxCh != nil || len(v.FxEffects) > 0 || v.FxErr != "" {
		return false
	}
	ch := make(chan fxLoadResult, 1)
	v.FxCh = ch
	model := v.Model
	file := v.ModelFile
	tree := ui.ModTree
	// Whatever the sound worker decoded already is handed over, because both
	// read the same .whe and the samples would otherwise be decoded twice.
	// The maps of the last load ride along for the same reason, and the sound
	// half is left to the worker that is usually decoding these very samples
	// while the FX button is being pressed.
	opts := fxLoadOpts{
		decoded: v.FxSound.Decoded(),
		prev:    v.FxTextures,
		audio:   v.FxSound == nil && v.FxSoundCh == nil,
	}
	go func() {
		ch <- loadModelEffects(tree, model, file, opts)
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
func FxStartSoundLoad(ui *uiState, v *modelViewer) bool {
	if v == nil || ui.ModTree == nil || v.ModelFile == "" {
		return false
	}
	if v.FxSoundCh != nil || v.FxSound != nil {
		return false
	}
	ch := make(chan fxLoadResult, 1)
	v.FxSoundCh = ch
	file := v.ModelFile
	tree := ui.ModTree
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
func FxReload(ui *uiState, v *modelViewer, doc *whe.Document) {
	if v == nil || v.Model == nil || ui.ModTree == nil || v.ModelFile == "" {
		return
	}
	model, file, tree := v.Model, v.ModelFile, ui.ModTree
	decoded := v.FxSound.Decoded()
	// The sound worker is started first, so the effects worker below can see
	// that somebody is already decoding this document's samples and leave the
	// sounds to it. Starting the effects worker first would hide the worker
	// that is about to start and have both of them pay for the same decode.
	if v.FxSoundCh == nil {
		if v.FxSound != nil {
			v.FxSound.Close()
			v.FxSound = nil
		}
		ch := make(chan fxLoadResult, 1)
		v.FxSoundCh = ch
		go func() {
			ch <- loadModelSoundsWhe(tree, file, doc, decoded)
		}()
	}
	if v.FxCh == nil {
		ch := make(chan fxLoadResult, 1)
		v.FxCh = ch
		opts := fxLoadOpts{
			decoded: decoded,
			prev:    v.FxTextures,
			audio:   v.FxSound == nil && v.FxSoundCh == nil,
		}
		go func() {
			ch <- loadModelEffectsWhe(tree, model, file, opts, doc)
		}()
	}
	log.Printf("3D preview: fx reload for an edited .whe (live document=%v)", doc != nil)
}

// fxApply takes the worker's answer: the effects it resolved, their maps, and
// whatever it could not read.
func FxApply(v *modelViewer, res fxLoadResult) {
	v.FxCh = nil
	if res.Err != nil {
		v.FxErr = res.Err.Error()
	} else {
		v.FxErr = ""
	}
	v.FxEffects = res.Effects
	v.FxTextures = res.Textures
	if len(res.Timelines) > 0 {
		v.FxTimelines = res.Timelines
	}
	log.Printf("3D preview: Effects resolved: %d Effects, %d sprite Textures", len(res.Effects), len(res.Textures))
	if v.FxErr != "" {
		log.Printf("3D preview: effects unavailable: %v", v.FxErr)
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
	if v.FxSound == nil {
		FxApplySounds(v, res, res.Timelines)
	}
	v.FxLast = time.Time{}
	v.FxOverlay = nil
}

// fxApplySoundsOnly takes the sound half of the work on its own, for the
// worker that reads the audio without resolving any effects.
func FxApplySoundsOnly(v *modelViewer, res fxLoadResult) {
	FxApplySounds(v, res, res.Timelines)
}

// fxApplySounds builds the sound player from what the worker resolved, and
// hands it the timeline so that a sound starts at the moment the clip reaches
// it rather than every frame.
func FxApplySounds(v *modelViewer, res fxLoadResult, timelines []whe.FxTimeline) {
	// The timelines are kept whatever the sounds turned out to be: the scrub
	// bar reads them to mark the frames a clip fires something on, and a model
	// with effects but no audio would otherwise lose them.
	v.FxTimelines = timelines
	if len(res.SoundBundles) == 0 {
		return
	}
	v.FxSound = fx.NewSoundPlayer(res.SoundBundles, timelines, res.Sounds.Snapshot())
	v.FxSoundOn = true
	log.Printf("3D preview: sounds wired: %d events, %d samples decoded",
		len(res.SoundBundles), res.Sounds.Len())
}

// fxSoundTick tells the sound player how far into the clip on screen the
// playhead is, which is what starts a sound at the moment its timeline names
// rather than every frame or only at the beginning.
//
// It is called every frame the clip is on, including while the clip is paused:
// a step forward over a footstep's moment is a sound a preview must make, and
// waiting for play to resume would lose it. DATAMTON event moments are in
// seconds, so the normalized playhead is converted using this clip's duration.
func FxSoundTick(v *modelViewer, anim string) {
	if v == nil || v.FxSound == nil {
		return
	}
	v.FxSound.SetEnabled(v.FxSoundOn)
	duration := animClipSeconds(v.Anim)
	v.FxSound.SetClipSeconds(duration)
	v.FxSound.Advance(anim, engine.ClampFloat64(float64(v.Time)*duration, 0, duration))
}

// fxOrigins moves every effect to where its marker is under the pose on show.
// A marker on a bone follows that bone, so an effect written for a weapon
// barrel stays on the barrel while the animation runs instead of hanging in
// the air where the model was when it was pressed.
func FxOrigins(v *modelViewer) {
	if v == nil || v.Model == nil {
		return
	}
	pose := Pose(v)
	for i := range v.FxEffects {
		e := &v.FxEffects[i]
		e.TurnAxes = [3]fauxgl.Vector{fxAxisX, fxAxisY, fxAxisZ}
		e.TurnAngles = [3]float64{v.FxRotX * math.Pi / 180, v.FxRotY * math.Pi / 180, v.FxRotZ * math.Pi / 180}
		// Keep a complete frame even when this effect's event does not name
		// a marker on the model. ModelMarkerFrame uses the same Z-up basis as
		// the model viewer; FX values stay local until transformed below.
		e.Frame = fxFallbackFrame
		if world, ok := v.Model.MarkerWorld(e.Marker, pose, true); ok {
			e.Origin = world
		}
		e.ManualOrigin = fauxgl.Vector{}
		e.HasManualOrigin = false
		e.ShowDirection = false
		e.ShowMarker = false
		if override, ok := v.FxOverrides[i]; ok {
			e.ManualOrigin = override.Origin
			e.HasManualOrigin = override.HasOrigin
			e.ShowDirection = override.ShowDirection
			e.ShowMarker = override.ShowMarker
			if override.HasOrigin {
				e.Origin = override.Origin
			}
			e.ScaleDistance = override.ScaleDistance
			// FX attributes are expressed in model/viewer XYZ, not the
			// attached marker's local frame. TurnAxes remains the world basis
			// initialized above, so Rotate X/Y/Z always follows the gizmo.
			e.TurnAngles = [3]float64{override.Rotation[0] * math.Pi / 180, override.Rotation[1] * math.Pi / 180, override.Rotation[2] * math.Pi / 180}
		}
		// The far end of a beam follows its own marker, which is often on
		// the same bone a few steps along, so the ribbon keeps its length
		// as the weapon swings.
		if e.HasOther {
			if world, ok := v.Model.MarkerWorld(e.OtherMarker, pose, true); ok {
				e.Far = world
			}
		} // An authored FX direction takes precedence over a marker-derived
		// fallback and is transformed by the same attachment frame as spray
		// offsets. Without one, a free beam follows its marker's forward axis.
		if e.Sim != nil && e.Sim.Axis.Length() > 1e-6 {
			e.Axis = e.FxMarkerVector(e.Sim.Axis)
		} else if axis, ok := v.Model.MarkerBeamAxis(e.Marker, pose, true); ok {
			// A beam runs along the tube it plays at, which is the marker's
			// second frame column, not the direction MarkerAxis scores the
			// marker as facing: see MarkerBeamAxis.
			e.Axis = axis
		}

		if right, up, ok := v.Model.MarkerBasis(e.Marker, pose, true); ok {
			e.Right = right
			e.Up = up
		}
		if e.Sim != nil && e.Sim.NoRotate {
			// Preserve the coordinate-system swap while discarding the marker
			// transform: this is the identity attachment frame, not raw file XYZ.
			e.Frame = fxFallbackFrame
		} else if x, y, z, ok := v.Model.MarkerFrameAxes(e.Marker, pose, true); ok {
			e.Frame = [3]fauxgl.Vector{x, y, z}
		}
		if e.AlignToOther && e.HasOther && (e.Sim == nil || !e.Sim.NoRotate) {
			e.Frame = fxFrameToward(e.Frame, e.Far.Sub(e.Origin))
		}
		if override, ok := v.FxOverrides[i]; ok {
			e.TurnAngles = [3]float64{override.Rotation[0] * math.Pi / 180, override.Rotation[1] * math.Pi / 180, override.Rotation[2] * math.Pi / 180}
		}
		if e.Ground && !e.HasManualOrigin {
			e.Origin = fxGroundOrigin(e.Origin, float64(v.Model.BoundMin[2]))
		}
		if override, ok := v.FxOverrides[i]; ok && override.HasScaleDistance {
			centre, _ := v.Model.ModelCenter()
			delta := e.Origin.Sub(centre)
			if length := delta.Length(); length > 1e-6 {
				e.Origin = centre.Add(delta.MulScalar(override.ScaleDistance / length))
			} else {
				e.Origin = centre.Add(fauxgl.Vector{Z: override.ScaleDistance})
			}
		}
		if override, ok := v.FxOverrides[i]; ok {
			e.ShowMarker = e.ShowMarker || override.ShowMarker
		}
		if e.ShowDirection && e.Axis.Length() > 1e-6 {
			e.Direction = e.Axis.Normalize()
		} else {
			e.Direction = fauxgl.Vector{}
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

var fxFallbackFrame = uistate.FxFallbackFrame

// fxRotated forwards to uistate, which needs it for the shared state.
func fxRotated(v fauxgl.Vector, rx, ry, rz float64, axes [3]fauxgl.Vector) fauxgl.Vector {
	return uistate.FxRotated(v, rx, ry, rz, axes)
}

// fxRotateAbout forwards to uistate, which needs it for the shared state.
func fxRotateAbout(v, axis fauxgl.Vector, angle float64) fauxgl.Vector {
	return uistate.FxRotateAbout(v, axis, angle)
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
func FxAdvance(v *modelViewer, now time.Time, w, h int, opts render.RenderOptions, softwareFX bool) bool {
	anim := ""
	if v != nil && v.Anim != nil {
		anim = v.Anim.Name
	}
	FxSelectionReset(v, anim)
	if !v.ShowFX || len(v.FxEffects) == 0 || w < 1 || h < 1 {
		v.FxOverlay = nil
		v.FxScene = nil
		return false
	}
	if v.FxLast.IsZero() {
		v.FxLast = now
	}
	// Only the effects the clip on screen fires are drawn: a .whe's timelines
	// put the build smoke and the death smoke on their own clips, and playing
	// them over an idle is what put a grey cloud above the model instead of
	// the green glow inside it.
	// The sounds move with the clip: a timeline says a footstep sounds at 0.47
	// of the way through the walk, and this is where the playhead is. It is
	// advanced before the effects are looked at, because a model may declare
	// sounds and no visible effects at all.
	FxSoundTick(v, anim)
	active := FxActiveEffects(v, anim)
	// A clip that no timeline names draws nothing, and says so once. It used to
	// fall back to playing every effect the model has, on the grounds that a
	// blank viewport is worse than a busy one. That is what took the GPU down:
	// a monolith carries 189 effects, and a clip the .whe never mentions drew
	// all of them at once — 189 beams, rings and sprays submitted every frame,
	// which a card answers by hanging and dropping the device (vkQueueSubmit
	// error -4). Showing nothing on a clip nothing drives is cheap, correct and
	// survivable; drawing everything is neither.
	if len(active) == 0 && len(v.FxEffects) > 0 && anim != "" && v.FxAnim != anim {
		log.Printf("3D preview: no timeline drives %q; it has no effects of its own", anim)
	}
	if len(active) == 0 {
		v.FxOverlay = nil
		v.FxScene = nil
		v.FxLast = now
		return false
	}
	// The clip on show changed: its effects start again from nothing, the way
	// a timeline's entry events play on the frame the clip begins.
	if v.FxAnim != anim {
		v.FxAnim = anim
		for i := range v.FxEffects {
			if v.FxEffects[i].FxPlaysFor(anim) && v.FxEffects[i].Sim != nil {
				v.FxEffects[i].Sim.Reset()
			}
		}
		v.FxLast = time.Time{}
	}
	if v.FxLast.IsZero() {
		v.FxLast = now
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
	dt := now.Sub(v.FxLast).Seconds()
	v.FxLast = now
	effects := FxStepOnClip(v, active, anim, dt)
	FxOrigins(v)
	// The scale is applied to each effect's local geometry rather than to its
	// marker origin or live simulation, so changing it keeps the effect attached
	// to the same model point without resetting particles in flight.
	scale := v.FxScale
	if scale <= 0 {
		scale = 1
	}
	effects = fxScaledEffects(effects, scale)
	// The camera's own up, which is what a billboard ring squares itself to.
	_, camUp := render.FxCameraBasis(opts)
	if softwareFX && v.ShowFX {
		v.FxOverlay = renderFxOverlay(effects, opts, w, h, v.FxTextures)
	} else {
		// Not this frame's picture. Keeping last frame's would paint
		// stale particles beside a hardware frame that carries its own.
		v.FxOverlay = nil
	}
	if v.ShowFX {
		v.FxScene = fxBuildGpuScene(effects, v.FxTextures, render.FxEyePosition(opts), camUp)
	} else {
		v.FxScene = nil
	}
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
func FxStepOnClip(v *modelViewer, active []fxOverlayEffect, anim string, dt float64) []fxOverlayEffect {
	clipLen := 1.0
	if v.Anim != nil {
		// How long one pass of the playhead actually takes, in seconds. It is
		// the frame count over the rate, not the reciprocal of the rate: the
		// playhead advances by dt*rate/frames, so it runs 0 to 1 over
		// frames/rate seconds. Using the rate alone made this twenty times too
		// short for a four frame clip, and since the step is the playhead's own
		// travel the effects crawled to a fifth of real time while the clip
		// ran — which reads as the effects freezing exactly when the animation
		// is playing, and behaving normally while it is paused, because the
		// paused path steps by the wall clock instead.
		clipLen = animClipSeconds(v.Anim)
	}
	if clipLen <= 0 {
		clipLen = 1
	}
	nowClip := float64(v.Time) * clipLen
	prevClip := v.FxClipPrev
	started := v.FxClipPrevAnim == anim
	// A different clip, or a playhead that moved back — which is both a rewind
	// and the clip looping — is a fresh pass, so what has run does not apply.
	if !started || nowClip+1e-9 < prevClip {
		for i := range v.FxEffects {
			if v.FxEffects[i].Sim != nil {
				v.FxEffects[i].Sim.Reset()
			}
		}
		// Just short of where the playhead is, so that an event written at the
		// start of the clip counts as reached on this very frame. Starting the
		// previous position at the playhead itself would step over it.
		prevClip = nowClip - 1e-9
	}
	v.FxClipPrev = nowClip
	v.FxClipPrevAnim = anim

	// While the clip is running the playhead is the clock. While it is paused
	// the playhead does not move, and wall time is what keeps an idle glow
	// alive; the two agree closely when it is playing, since the viewer
	// advances the playhead from the same clock.
	step := nowClip - prevClip
	if !v.Playing {
		step = dt
	}
	var out []fxOverlayEffect
	for i := range active {
		e := &active[i]
		if e.Sim == nil {
			continue
		}
		fire := e.FxFireTime(anim)
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
func FxSelectionAnimName(v *modelViewer, anim string) string {
	if v == nil {
		return anim
	}
	if anim == "" && v.Anim != nil {
		anim = v.Anim.Name
	}
	if !strings.EqualFold(v.FxSelectionAnim, anim) {
		v.FxSelectionAnim = anim
		v.FxManualOn = nil
		v.FxManualOff = nil
		v.FxAttrClicks = nil
		v.FxAttrIndex = -1
		v.FxAttrOpen = false
	}
	return anim
}

func FxSelectionReset(v *modelViewer, anim string) {
	if v != nil {
		FxSelectionAnimName(v, anim)
	}
}

func FxEffectSelected(v *modelViewer, index int, anim string) bool {
	if v == nil || index < 0 || index >= len(v.FxEffects) {
		return false
	}
	anim = FxSelectionAnimName(v, anim)
	if v.FxManualOff[index] {
		return false
	}
	return v.FxManualOn[index] || v.FxEffects[index].FxPlaysFor(anim)
}

func FxEffectVisible(v *modelViewer, index int) bool {
	if v == nil || index < 0 || index >= len(v.FxEffects) {
		return false
	}
	return v.FxEffects[index].FxPlaysFor(v.FxSelectionAnim)
}

func FxSetEffectSelected(v *modelViewer, index int, anim string, selected bool) {
	if v == nil || index < 0 || index >= len(v.FxEffects) {
		return
	}
	anim = FxSelectionAnimName(v, anim)
	if selected {
		if v.FxManualOn == nil {
			v.FxManualOn = make(map[int]bool)
		}
		v.FxManualOn[index] = true
		if v.FxManualOff != nil {
			delete(v.FxManualOff, index)
		}
	} else {
		if v.FxManualOn != nil {
			delete(v.FxManualOn, index)
		}
		if v.FxManualOff == nil {
			v.FxManualOff = make(map[int]bool)
		}
		v.FxManualOff[index] = true
	}
	Invalidate(v)
}

// fxActiveEffects returns this clip's timeline-selected effects plus any
// explicitly checked effects. Timeline selection controls when an effect
// starts, even if a manual check adds it on a clip whose timeline does not
// name it; such an effect starts at the clip's beginning.
func FxActiveEffects(v *modelViewer, anim string) []fxOverlayEffect {
	if v == nil || len(v.FxEffects) == 0 {
		return nil
	}
	anim = FxSelectionAnimName(v, anim)
	var out []fxOverlayEffect
	for i := range v.FxEffects {
		if !FxEffectSelected(v, i, anim) {
			continue
		}
		e := v.FxEffects[i]
		if e.Timed && !e.FxPlaysFor(anim) {
			e.Timed = false
			e.FireTimes = nil
		}
		out = append(out, e)
	}
	return out
}

// fxActiveIndices returns indices in the same stable order as fxEffects.
func FxActiveIndices(v *modelViewer, anim string) []int {
	var out []int
	if v == nil {
		return nil
	}
	anim = FxSelectionAnimName(v, anim)
	for i := range v.FxEffects {
		if FxEffectSelected(v, i, anim) {
			out = append(out, i)
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

type fxAttributeOverride = uistate.FxAttributeOverride

type fxOverlayEffect = uistate.FxOverlayEffect

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
		sp.Radius = render.SpriteFallbackRadius
	}
	sp.Radius *= scale
	return sp
}

// fxRotateVectorBetween forwards to uistate, which needs it for the shared state.
func fxRotateVectorBetween(v, from, to fauxgl.Vector) fauxgl.Vector {
	return uistate.FxRotateVectorBetween(v, from, to)
}

// fxFrameToward rotates the local FX frame so renderer-local +Z follows the
// direction selected by fx_transform = self_to_other. Rotating all three
// axes together preserves the marker's roll and the handedness of the FX
// coordinate conversion.
func fxFrameToward(frame [3]fauxgl.Vector, direction fauxgl.Vector) [3]fauxgl.Vector {
	from, to := frame[2].Normalize(), direction.Normalize()
	if from.Length() < 1e-6 || to.Length() < 1e-6 {
		return frame
	}
	cross := from.Cross(to)
	sin := cross.Length()
	cos := engine.ClampFloat64(from.Dot(to), -1, 1)
	if sin < 1e-6 {
		if cos > 0 {
			return frame
		}
		// A half-turn has no unique cross-product axis. Use the marker's X
		// axis, projected perpendicular to the old forward direction.
		axis := frame[0].Sub(from.MulScalar(frame[0].Dot(from))).Normalize()
		if axis.Length() < 1e-6 {
			axis = fxFallbackFrame[0].Sub(from.MulScalar(fxFallbackFrame[0].Dot(from))).Normalize()
		}
		if axis.Length() < 1e-6 {
			return frame
		}
		for i, v := range frame {
			frame[i] = axis.MulScalar(2 * axis.Dot(v)).Sub(v)
		}
		return frame
	}
	axis := cross.DivScalar(sin)
	for i, v := range frame {
		frame[i] = v.MulScalar(cos).
			Add(axis.Cross(v).MulScalar(sin)).
			Add(axis.MulScalar(axis.Dot(v) * (1 - cos)))
	}
	return frame
}

func fxOrientSprite(sp fx.Sprite, frame [3]fauxgl.Vector) fx.Sprite {
	if !sp.Horizontal {
		return sp
	}
	normal := fauxgl.Vector{X: sp.PlaneNormal[0], Y: sp.PlaneNormal[1], Z: sp.PlaneNormal[2]}
	if normal.Length() < 1e-6 {
		// A ring particle lies in the file's XY plane, so local file Z is
		// its normal. The FX-to-model axis swap carries that to model Y.
		normal = fauxgl.Vector{Y: 1}
	}
	sp.PlaneNormal = [3]float64{normal.X, normal.Y, normal.Z}
	return sp
}

const fxBeamFreeLength = uistate.FxBeamFreeLength

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
func renderFxOverlay(effects []fxOverlayEffect, opts render.RenderOptions, w, h int, textures map[string]*image.RGBA) *image.RGBA {
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	if len(effects) == 0 || w < 1 || h < 1 {
		return out
	}
	view := render.BuildViewMatrix(opts)
	// The camera's own right and up, so that a quad drawn along them faces
	// the eye square on. They come out of the same orbit the frame is drawn
	// with: the direction from the target to the eye, and the Z-up the
	// models are authored in.
	right, up := render.FxCameraBasis(opts)
	eye := render.FxEyePosition(opts)
	for _, e := range effects {
		if e.Sim == nil {
			continue
		}
		// Beams first: they are the long shapes a particle is drawn on top
		// of. Keep marker-to-marker distances fixed while scaling authored
		// dimensions and local offsets, just as in the GPU scene builder.
		spansMarkers := e.FxSpansMarkers()
		for _, rb := range e.Sim.Beams(e.Origin, e.FxFarPoint(), spansMarkers) {
			rb = e.FxMarkerRibbon(rb)
			if e.TurnAngles != [3]float64{} {
				rb.From = e.Origin.Add(e.FxTurnVector(rb.From.Sub(e.Origin)))
				rb.To = e.Origin.Add(e.FxTurnVector(rb.To.Sub(e.Origin)))
			}
			rb = e.FxScaleRibbon(rb, spansMarkers)
			if rb.Alpha > 0 && rb.Width > 0 {
				drawFxBeam(out, view, eye, w, h, rb, textures)
			}
		}
		axX, axY, axZ := e.FxRingFrame()
		markerFrame := [3]fauxgl.Vector{axX, axY, axZ}
		for _, rb := range e.Sim.Rings(e.Origin, axX, axY, axZ, up) {
			rb = e.FxScaleRibbon(rb, false)
			if rb.Alpha > 0 && rb.Width > 0 {
				drawFxBeam(out, view, eye, w, h, rb, textures)
			}
		}
		for _, sp := range e.Sim.Sprites() {
			sp = e.FxTurnSprite(sp)
			sp = fxOrientSprite(sp, markerFrame)
			world := fauxgl.Vector{
				X: e.Origin.X + sp.Pos[0],
				Y: e.Origin.Y + sp.Pos[1],
				Z: e.Origin.Z + sp.Pos[2],
			}
			sp = fxScaleSprite(sp, e.FxScaleOrOne())
			if sp.Horizontal {
				drawFxHorizontalSprite(out, view, w, h, world, sp.Radius, sp, textures)
			} else {
				drawFxBillboard(out, view, right, up, w, h, world, sp.Radius, sp, textures)
			}
		}
	}
	return out
}

// render.SpriteFallbackRadius, render.FxCameraBasis and render.FxEyePosition moved to
// internal/app/render: the GPU renderer needs them beside render.RenderOptions.

// drawFxHorizontalSprite projects a fixed XY-plane sprite, matching the
// non-billboard STYLE_RING plane the game authors perpendicular to world Z.
func drawFxHorizontalSprite(dst *image.RGBA, view fauxgl.Matrix, w, h int, world fauxgl.Vector, radius float64, sp fx.Sprite, textures map[string]*image.RGBA) {
	if radius <= 0 {
		radius = render.SpriteFallbackRadius
	}
	normal := fauxgl.Vector{X: sp.PlaneNormal[0], Y: sp.PlaneNormal[1], Z: sp.PlaneNormal[2]}
	if normal.Length() < 1e-6 {
		normal = fauxgl.Vector{Z: 1}
	}
	normal = normal.Normalize()
	help := fauxgl.Vector{Z: 1}
	if math.Abs(normal.Z) > 0.95 {
		help = fauxgl.Vector{Y: 1}
	}
	baseRight := help.Cross(normal).Normalize()
	baseUp := normal.Cross(baseRight).Normalize()
	c, s := math.Cos(sp.Rotation), math.Sin(sp.Rotation)
	right := baseRight.MulScalar(c).Add(baseUp.MulScalar(s))
	up := baseUp.MulScalar(c).Sub(baseRight.MulScalar(s))
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
		engine.ClampFloat64(sp.R, 0, 1)*engine.ClampFloat64(sp.A, 0, 1),
		engine.ClampFloat64(sp.G, 0, 1)*engine.ClampFloat64(sp.A, 0, 1),
		engine.ClampFloat64(sp.B, 0, 1)*engine.ClampFloat64(sp.A, 0, 1),
		engine.ClampFloat64(sp.A, 0, 1), sp.Additive, w, h)
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
	alpha := engine.ClampFloat64(sp.A, 0, 1)
	if alpha <= 0 {
		return
	}
	cr := engine.ClampFloat64(sp.R, 0, 1) * alpha
	cg := engine.ClampFloat64(sp.G, 0, 1) * alpha
	cb := engine.ClampFloat64(sp.B, 0, 1) * alpha
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
		dst.Pix[i] = engine.ClampByte(float64(dst.Pix[i]) + r*255)
		dst.Pix[i+1] = engine.ClampByte(float64(dst.Pix[i+1]) + g*255)
		dst.Pix[i+2] = engine.ClampByte(float64(dst.Pix[i+2]) + b*255)
		dst.Pix[i+3] = engine.ClampByte(math.Max(float64(dst.Pix[i+3]), a*255))
		return
	}
	// Over: the sprite in front of what is already there.
	in := a * 255
	if in <= 0 {
		return
	}
	keep := 1 - a
	dst.Pix[i] = engine.ClampByte(r*255 + float64(dst.Pix[i])*keep)
	dst.Pix[i+1] = engine.ClampByte(g*255 + float64(dst.Pix[i+1])*keep)
	dst.Pix[i+2] = engine.ClampByte(b*255 + float64(dst.Pix[i+2])*keep)
	dst.Pix[i+3] = engine.ClampByte(in + float64(dst.Pix[i+3])*keep)
}

// clampByte moved to internal/3d_engine as engine.ClampByte.

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

type fxLoadResult = uistate.FxLoadResult

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

// fxLoadOpts is what a caller already holds when it asks for a load: the
// samples the sound worker decoded, the maps the previous load decoded, and
// whether this load is the one that has to do the sound half at all.
type fxLoadOpts struct {
	// decoded are samples another worker has already decoded. They are handed
	// over rather than decoded again, because both workers read the same .whe.
	decoded map[string]*fx.SoundSample
	// prev are the maps the last load decoded. An effect's sprites do not
	// change because an event was edited, so a reload only pays for the maps
	// it has not seen.
	prev map[string]*image.RGBA
	// audio says whether this load resolves and decodes the .whe's sounds.
	// It is false whenever somebody else is already doing that work: a sound
	// worker in flight is decoding these very samples, and paying for them
	// twice is what made an FX load wait seconds for work already underway.
	audio bool
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
func loadModelEffects(tree *modtree.ModTree, model *WhmModel, modelFile string, opts fxLoadOpts) fxLoadResult {
	return loadModelEffectsWhe(tree, model, modelFile, opts, nil)
}

// loadModelEffectsWhe resolves the model's effects with the live .whe
// document in hand when there is one: its definitions and its timelines are
// what the effects are built from, so values edited but not yet saved are the
// ones the preview shows.
func loadModelEffectsWhe(tree *modtree.ModTree, model *WhmModel, modelFile string, opts fxLoadOpts, liveWhe *whe.Document) fxLoadResult {
	res := fxLoadResult{
		Textures:     map[string]*image.RGBA{},
		Sounds:       fx.NewSoundCache(),
		SoundBundles: map[string]fx.SoundBundle{},
	}
	// Samples the sound-only worker already decoded are handed over rather
	// than decoded again. Both workers read the same .whe, so without this the
	// effect of the FX button would be to pay for every sample of the model a
	// second time.
	if len(opts.decoded) > 0 {
		for k, s := range opts.decoded {
			res.Sounds.Put(k, s)
		}
	}
	if tree == nil || model == nil {
		return res
	}
	loadStart := time.Now()
	bindings, _, err := fx.ResolveEventsForModel(tree, modelFile)
	if err != nil {
		res.Err = err
		return res
	}
	tEvents := time.Since(loadStart)
	if liveWhe != nil {
		bindings = fx.WheBindingsOverlay(bindings, liveWhe)
	}
	roots := materialSearchRoots(tree)
	seen := map[string]*image.RGBA{}
	// The maps the last load already decoded are the starting point, so a
	// reload after an edit does not decode the same seventy sprites again.
	for path, img := range opts.prev {
		if img != nil {
			seen[path] = img
		}
	}
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
	tTimelines := time.Since(loadStart)
	var tLua, tTex time.Duration
	var tRead, tParse, tSim time.Duration
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
	// One effect file serves several events — the monolith's 189 events name
	// just 75 files — so each is read and parsed once and every event builds
	// its own simulation from that shared parse. An FxFile is plain data and
	// NewFxSim reads it without writing to it, so the parsed copy is safe to
	// hand to as many simulations as name it.
	parsedFx := map[string]*fx.FxFile{}
	for _, b := range bindings {
		fxPath, ok := b.Event.First("fx")
		if !ok || fxPath == "" {
			continue
		}
		luaStart := time.Now()
		key := strings.ToLower(fxLuaPath(fxPath))
		file := parsedFx[key]
		if file == nil {
			readStart := time.Now()
			data, err := readFxLuaFile(tree, roots, fxPath)
			tRead += time.Since(readStart)
			if err != nil {
				log.Printf("3D preview: effect %q: %v", fxPath, err)
				continue
			}
			parseStart := time.Now()
			file, err = fx.ParseFxLua(data)
			tParse += time.Since(parseStart)
			if err != nil {
				log.Printf("3D preview: effect %q: %v", fxPath, err)
				continue
			}
			parsedFx[key] = file
		}
		simStart := time.Now()
		sim := fx.NewFxSim(file)
		tSim += time.Since(simStart)
		tLua += time.Since(luaStart)
		marker := fxEventMarker(model, b.Event, len(res.Effects))
		other := fxBeamOtherMarker(model, b.Event)
		e := fxOverlayEffect{
			Sim:          sim,
			Label:        b.Event.Name,
			FxName:       fxPath,
			Marker:       marker,
			Ground:       fxIsGroundEffect(b.Event, fxPath),
			HasOther:     other >= 0,
			OtherMarker:  other,
			Spans:        fxEventSpansDistance(b.Event),
			AlignToOther: fxEventAlignsToOther(b.Event),
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
		e.Frame = fxFallbackFrame
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
		if axis, ok := model.MarkerBeamAxis(marker, rest, true); ok {
			e.Axis = axis
		} else if sim != nil && sim.Axis.Length() > 1e-6 {
			e.Axis = e.FxMarkerVector(sim.Axis)
		}
		if sim != nil && sim.NoRotate {
			// No_Rotate overrides marker orientation, not the FX file's
			// required Y-up to model Z-up axis conversion.
			e.Frame = fxFallbackFrame
		} else if x, y, z, ok := model.MarkerFrameAxes(marker, rest, true); ok {
			e.Frame = [3]fauxgl.Vector{x, y, z}
		}
		if right, up, ok := model.MarkerBasis(marker, rest, true); ok {
			e.Right = right
			e.Up = up
		}
		if e.AlignToOther && sim != nil && !sim.NoRotate {
			e.Frame = fxFrameToward(e.Frame, e.Far.Sub(e.Origin))
		}
		res.Effects = append(res.Effects, e)
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
			texStart := time.Now()
			img, err := decodeFxTexture(tree, roots, path)
			tTex += time.Since(texStart)
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
			texStart := time.Now()
			img, err := decodeFxTexture(tree, roots, path)
			tTex += time.Since(texStart)
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
			res.Textures[path] = img
		}
	}
	tEffects := time.Since(loadStart)
	// The timelines are this loader's own answer either way: the scrub bar
	// reads them whatever becomes of the sounds, and the sound worker's own
	// result carries the same ones out of the same file.
	res.Timelines = timelines
	if opts.audio {
		fxLoadWheAudio(tree, modelFile, liveWhe, &res)
	}
	log.Printf("3D preview: fx load in %v: events %v, timelines %v, effects %v (read %v, parse %v, sim %v, textures %v), sounds %v; %d bindings, %d effects, %d textures from %d fx files",
		time.Since(loadStart), tEvents, tTimelines-tEvents,
		tEffects-tTimelines, tRead, tParse, tSim, tTex, time.Since(loadStart)-tEffects,
		len(bindings), len(res.Effects), len(res.Textures), len(parsedFx))
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
		Sounds:       fx.NewSoundCache(),
		SoundBundles: map[string]fx.SoundBundle{},
	}
	if tree == nil {
		return res
	}
	if len(decoded) > 0 {
		for k, s := range decoded {
			res.Sounds.Put(k, s)
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
	// The bytes are already in hand, so the timelines come out of them: reading
	// the .whe a second time for this would parse a file of chunks again for
	// an answer the line above just produced.
	loadWheSoundsFrom(tree, data, whe.ParseFxTimelines(data), res)
}

// loadWheSoundsFrom is loadWheSounds with the .whe bytes and the timelines
// handed in: the live document being edited marshals its own, so an unsaved
// sound edit is what gets decoded and wired.
func loadWheSoundsFrom(tree *modtree.ModTree, data []byte, timelines []whe.FxTimeline, res *fxLoadResult) {
	// The timelines come first: they are the scrub bar's own answer and do not
	// depend on there being any sound to play. Returning below on an empty
	// sound list without them would hand the caller a set that clears what the
	// other worker has already put there.
	res.Timelines = timelines
	// One worker per sound that names a folder of variants, all of them
	// finished before this returns: the samples are handed to the player as one
	// set once it does.
	var fills sync.WaitGroup
	declared := whe.ParseWheSounds(data)
	if len(declared) == 0 {
		return
	}
	soundStart := time.Now()
	// Which events the timelines on screen can fire at all, so the samples for
	// the rest are never decoded.
	played := map[string]bool{}
	for _, tl := range timelines {
		for _, ev := range tl.Events {
			played[strings.ToLower(ev.Name)] = true
		}
	}
	roots := materialSearchRoots(tree)
	// Declared but never fired is normal rather than a failure: the sound
	// belongs to a state this model is not in.
	type soundJob struct {
		name string
		ev   whe.FxSoundEvent
		b    fx.SoundBundle
		err  error
	}
	var jobs []*soundJob
	for name, ev := range declared {
		if ev.Path == "" || len(played) == 0 && !fxSoundAlwaysPlays(name) {
			continue
		}
		if len(played) > 0 && !played[name] {
			continue
		}
		jobs = append(jobs, &soundJob{name: name, ev: ev})
	}
	// Resolving a bundle and decoding its first sample were paid one sound
	// after another — sixteen of them in a row is seconds of a worker waiting
	// on itself, and it is the whole of what this function costs. No two
	// sounds depend on each other, so they are resolved and decoded at once,
	// a few at a time per core, and only the wiring below runs serially,
	// because that is what writes the maps the caller is about to read.
	var wg sync.WaitGroup
	sem := make(chan struct{}, maxInt(2, runtime.GOMAXPROCS(0)))
	for _, j := range jobs {
		wg.Add(1)
		go func(j *soundJob) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			b, ok := fx.ResolveSound(tree, roots, j.ev.Path)
			if !ok || len(b.Files) == 0 {
				j.err = whe.SoundMissing(j.ev.Path)
				return
			}
			j.b = b
			first := b.Files[0]
			if _, done := res.Sounds.Get(first); done {
				return
			}
			s, err := fxSoundDecodeForPlayback(tree, roots, first)
			if err != nil {
				j.err = err
				return
			}
			res.Sounds.Put(first, s)
		}(j)
	}
	wg.Wait()
	for _, j := range jobs {
		if j.err != nil {
			log.Printf("3D preview: sound %q: %v", j.ev.Path, j.err)
			continue
		}
		res.SoundBundles[j.name] = j.b
		if len(j.b.Files) > 1 {
			fills.Add(1)
			go func(b fx.SoundBundle) {
				defer fills.Done()
				fxSoundFillVariants(tree, roots, b, res.Sounds)
			}(j.b)
		}
	}
	// The fills have to be finished before the result is handed on. They write
	// into the same map the caller is about to read and then keep, and a map
	// written by one goroutine while another reads it is not a map with a
	// missing key in it but a crash. The first sample of each sound is already
	// decoded by now, so this waits for the variants of a footfall folder and
	// not for the model's audio as a whole.
	waitStart := time.Now()
	fills.Wait()
	variantWait := time.Since(waitStart)
	log.Printf("3D preview: sounds in %v (resolve + first %v, variants %v): %d bundles",
		time.Since(soundStart), time.Since(soundStart)-variantWait, variantWait,
		len(res.SoundBundles))
}

// fxSoundAlwaysPlays says whether a sound the model's timelines never fire is
// still worth resolving: none are. It exists so the one place that decides has
// a name to read rather than a bare false.
func fxSoundAlwaysPlays(name string) bool { return false }

// fxSoundFillVariants decodes the rest of a sound's samples alongside the
// others, so a footfall folder of seven files is ready by the time the second
// step lands rather than one step at a time.
//
// A variant the outgoing player already holds is left alone: a reload hands
// the old samples over for exactly this reason, and decoding them again was
// seconds of work repeated on every edit to the .whe.
//
// Every fill for a model runs at once rather than one after another: the files
// are independent, they are decoded off the frame that asked for them, and a
// model with three sound folders would otherwise decode them in sequence for
// no gain. They write into one cache, so it is a fx.SoundCache rather than a
// bare map and the caller's Wait is what makes handing it over safe.
func fxSoundFillVariants(tree *modtree.ModTree, roots []string, b fx.SoundBundle, into *fx.SoundCache) {
	for _, f := range b.Files[1:] {
		if _, done := into.Get(f); done {
			continue
		}
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

// fxEventAlignsToOther reports the explicit event transform that aims an FX
// attachment from its own marker to the paired marker. Other transform modes
// are left unchanged until their distinct game semantics are implemented.
func fxEventAlignsToOther(ev whe.FxEvent) bool {
	v, ok := ev.First("fx_transform")
	return ok && strings.EqualFold(strings.TrimSpace(v), "self_to_other")
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

// fxIsGroundEffect identifies effects whose event metadata says their origin
// belongs on the ground. Do not infer this from STYLE_SPRAY or particle gravity:
// many smoke, exhaust and impact effects are meant to rise or fall in mid-air.
// Ground/dust names and explicit ground marker names are the reliable signals.
func fxIsGroundEffect(ev whe.FxEvent, fxPath string) bool {
	hasHint := func(s string) bool {
		s = strings.ToLower(strings.ReplaceAll(s, `\`, "/"))
		return strings.Contains(s, "ground") || strings.Contains(s, "dust")
	}
	groundMarker := false
	for _, key := range []string{"marker", "marker_0", "marker_surface_00", "marker_surface_0", "random_marker_0"} {
		if marker, ok := ev.First(key); ok && hasHint(marker) {
			groundMarker = true
			break
		}
	}
	if !hasHint(ev.Name) && !hasHint(fxPath) && !groundMarker {
		return false
	}
	// A ground-named lightning/beam is usually a strike that should travel
	// between markers or rise from its authored origin, not an emitter to pin
	// to the model's feet. Likewise, a named remote endpoint is already the
	// ground contact and must not pull the beam's source down.
	lowerName := strings.ToLower(ev.Name)
	lowerPath := strings.ToLower(fxPath)
	if strings.Contains(lowerName, "lightning") || strings.Contains(lowerName, "beam") ||
		strings.Contains(lowerPath, "lightning") || strings.Contains(lowerPath, "beam") {
		return false
	}
	if other, ok := ev.First("fx_other_marker"); ok && other != "" {
		return false
	}
	if other, ok := ev.First("fx_other"); ok && other != "" && !strings.EqualFold(other, "self") {
		return false
	}
	return true
}

// fxGroundOrigin preserves an effect's horizontal attachment while anchoring
// its origin to the model's lowest surface point.
func fxGroundOrigin(origin fauxgl.Vector, groundZ float64) fauxgl.Vector {
	origin.Z = groundZ
	return origin
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
	alpha := engine.ClampFloat64(rb.Alpha, 0, 1)
	if alpha <= 0 {
		return
	}
	cr := engine.ClampFloat64(rb.R, 0, 1) * alpha
	cg := engine.ClampFloat64(rb.G, 0, 1) * alpha
	cb := engine.ClampFloat64(rb.B, 0, 1) * alpha
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

// ─── merged from fx_marker_overlay.go ───────────────────────────────────────────────

const markerArrowWorldLength = 0.5

func PaintMarkerOverlay(v *modelViewer, gtx layout.Context, w, h int) {
	at, ok := MarkerHighlight(v, w, h)
	if !ok {
		return
	}
	paintMarkerRing(gtx, at)
	if tip, ok := MarkerArrow(v, w, h); ok {
		paintMarkerArrow(gtx, at, tip)
	}
}

func MarkerHighlight(v *modelViewer, w, h int) (fauxgl.Vector, bool) {
	if v == nil || v.Model == nil || v.MarkerIndex < 0 || v.MarkerIndex >= len(v.Model.Markers) {
		return fauxgl.Vector{}, false
	}
	world, ok := v.Model.MarkerWorld(v.MarkerIndex, Pose(v), true)
	if !ok {
		return fauxgl.Vector{}, false
	}
	p := fxProject(render.BuildViewMatrix(Options(v, w, h)), world, w, h)
	if p == nil {
		return fauxgl.Vector{}, false
	}
	return *p, true
}

func MarkerArrow(v *modelViewer, w, h int) (fauxgl.Vector, bool) {
	if v == nil || v.Model == nil || v.MarkerIndex < 0 || v.MarkerIndex >= len(v.Model.Markers) {
		return fauxgl.Vector{}, false
	}
	axis, ok := v.Model.MarkerAxis(v.MarkerIndex, Pose(v), true)
	if !ok {
		return fauxgl.Vector{}, false
	}
	world, ok := v.Model.MarkerWorld(v.MarkerIndex, Pose(v), true)
	if !ok {
		return fauxgl.Vector{}, false
	}
	view := render.BuildViewMatrix(Options(v, w, h))
	at := fxProject(view, world, w, h)
	tip := fxProject(view, world.Add(axis.MulScalar(markerArrowWorldLength)), w, h)
	if at == nil || tip == nil {
		return fauxgl.Vector{}, false
	}
	return *tip, true
}

func paintMarkerArrow(gtx layout.Context, from, to fauxgl.Vector) {
	drawBar := func(ax, ay, bx, by float64) {
		r := image.Rect(int(math.Min(ax, bx)), int(math.Min(ay, by)), int(math.Max(ax, bx))+1, int(math.Max(ay, by))+1)
		if r.Dx() < 2 {
			r.Min.X--
			r.Max.X = r.Min.X + 2
		}
		if r.Dy() < 2 {
			r.Min.Y--
			r.Max.Y = r.Min.Y + 2
		}
		a := clip.Rect(r).Push(gtx.Ops)
		paint.ColorOp{Color: uikit.CMarkerHi}.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
		a.Pop()
	}
	dx, dy := to.X-from.X, to.Y-from.Y
	length := math.Hypot(dx, dy)
	if length < 1 {
		return
	}
	ux, uy := dx/length, dy/length
	head := math.Min(11, length*0.45)
	baseX, baseY := to.X-ux*head, to.Y-uy*head
	const wing = 6.0
	drawBar(from.X, from.Y, baseX, baseY)
	drawBar(baseX-uy*wing, baseY+ux*wing, to.X, to.Y)
	drawBar(baseX+uy*wing, baseY-ux*wing, to.X, to.Y)
}

func paintMarkerRing(gtx layout.Context, at fauxgl.Vector) {
	const half, thick = 9.0, 2.0
	x0, y0 := int(at.X-half), int(at.Y-half)
	x1, y1 := int(at.X+half), int(at.Y+half)
	bars := [4]image.Rectangle{
		image.Rect(x0-thick/2, y0-thick/2, x1+thick/2, y0+thick/2),
		image.Rect(x0-thick/2, y1-thick/2, x1+thick/2, y1+thick/2),
		image.Rect(x0-thick/2, y0-thick/2, x0+thick/2, y1+thick/2),
		image.Rect(x1-thick/2, y0-thick/2, x1+thick/2, y1+thick/2),
	}
	for _, r := range bars {
		a := clip.Rect(r).Push(gtx.Ops)
		paint.ColorOp{Color: uikit.CMarkerHi}.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
		a.Pop()
	}
}

// paintFXAttributeMarkers draws optional origin rings and direction arrows for
// effects whose attribute editor has those diagnostics enabled.
func PaintFXAttributeMarkers(v *modelViewer, gtx layout.Context, w, h int) {
	if v == nil || v.Model == nil || w < 1 || h < 1 {
		return
	}
	view := render.BuildViewMatrix(Options(v, w, h))
	pose := Pose(v)
	for i := range v.FxEffects {
		e := &v.FxEffects[i]
		if !e.ShowMarker && !e.ShowDirection {
			continue
		}
		origin := e.Origin
		if ov, ok := v.FxOverrides[i]; ok && ov.HasOrigin {
			origin = ov.Origin
		}
		at := fxProject(view, origin, w, h)
		if at == nil {
			continue
		}
		if e.ShowMarker {
			paintMarkerRing(gtx, *at)
		}
		if e.ShowDirection {
			axis := e.Direction
			if axis.Length() < 1e-6 && e.Marker >= 0 {
				axis, _ = v.Model.MarkerAxis(e.Marker, pose, true)
			}
			if axis.Length() > 1e-6 {
				if tip := fxProject(view, origin.Add(axis.Normalize().MulScalar(markerArrowWorldLength)), w, h); tip != nil {
					paintMarkerArrow(gtx, *at, *tip)
				}
			}
		}
	}
}

// ─── merged from fx_axis_overlay.go ───────────────────────────────────────────────

const (
	fxAxesPairGap  = 52.0
	fxAxesTriadLen = 18.0
	fxAxesProbe    = 1.0
)

type fxOriginAxesEntry struct {
	origin fauxgl.Vector
	frames [2][3]fauxgl.Vector
}

func fxOriginAxesKey(origin fauxgl.Vector, frames [2][3]fauxgl.Vector) [21]uint64 {
	key := [21]uint64{math.Float64bits(origin.X), math.Float64bits(origin.Y), math.Float64bits(origin.Z)}
	at := 3
	for _, frame := range frames {
		for _, axis := range frame {
			key[at], key[at+1], key[at+2] = math.Float64bits(axis.X), math.Float64bits(axis.Y), math.Float64bits(axis.Z)
			at += 3
		}
	}
	return key
}

func fxOriginAxisFrames(e fxOverlayEffect, model *WhmModel, marker int, pose WhmPose) [2][3]fauxgl.Vector {
	local := fxFallbackFrame
	noRotate := e.Sim != nil && e.Sim.NoRotate
	if !noRotate && model != nil {
		if x, y, z, ok := model.MarkerFrameAxes(marker, pose, true); ok {
			local = [3]fauxgl.Vector{x, y, z}
		}
	}
	// The attribute editor's correction is applied to the effect geometry
	// after its attachment frame, so show the corrected local frame here too.
	for i := range local {
		local[i] = e.FxTurnVector(local[i])
	}
	return [2][3]fauxgl.Vector{local, {{X: 1}, {Y: 1}, {Z: 1}}}
}

func FxAxesButton(ui *uiState, gtx layout.Context, v *modelViewer) layout.Dimensions {
	if v == nil {
		return layout.Dimensions{}
	}
	if v.FxAxesClick.Clicked(gtx) {
		v.ShowFXAxes = !v.ShowFXAxes
		Invalidate(v)
	}
	label := "FX axes"
	if v.ShowFXAxes {
		label = "Axes on"
	}
	return SmallButton(ui, gtx, &v.FxAxesClick, label)
}

func ModelFxAxesOverlay(ui *uiState, gtx layout.Context, t *fileTab) layout.Dimensions {
	if t == nil || t.Model == nil || t.Model.Model == nil || !t.Model.ShowFXAxes {
		return layout.Dimensions{}
	}
	w, h := gtx.Constraints.Max.X, gtx.Constraints.Max.Y
	if w <= 0 || h <= 0 {
		return layout.Dimensions{}
	}
	return LayoutFXOriginAxes(t.Model, ui.Th, gtx, w, h)
}

func LayoutFXOriginAxes(v *modelViewer, th *material.Theme, gtx layout.Context, w, h int) layout.Dimensions {
	if v == nil || v.Model == nil || w <= 0 || h <= 0 {
		return layout.Dimensions{}
	}
	anim := ""
	if v.Anim != nil {
		anim = v.Anim.Name
	}
	FxSelectionAnimName(v, anim)
	indices := FxActiveIndices(v, anim)
	if len(indices) == 0 {
		return layout.Dimensions{}
	}
	view := render.BuildViewMatrix(Options(v, w, h))
	pose := Pose(v)
	origins := make(map[[21]uint64]fxOriginAxesEntry, len(indices))
	for _, index := range indices {
		e := &v.FxEffects[index]
		origin := e.Origin
		if override, ok := v.FxOverrides[index]; ok && override.HasOrigin {
			origin = override.Origin
		} else if p, ok := v.Model.MarkerWorld(e.Marker, pose, true); ok {
			origin = p
		}
		if e.Ground {
			if override, ok := v.FxOverrides[index]; !ok || !override.HasOrigin {
				origin = fxGroundOrigin(origin, float64(v.Model.BoundMin[2]))
			}
		}
		frames := fxOriginAxisFrames(*e, v.Model, e.Marker, pose)
		if e.HasManualOrigin && !e.ShowMarker && !e.ShowDirection {
			continue
		}
		origins[fxOriginAxesKey(origin, frames)] = fxOriginAxesEntry{origin: origin, frames: frames}
	}
	area := gtx.Constraints.Max
	canvasScale := math.Min(float64(area.X)/float64(w), float64(area.Y)/float64(h))
	if canvasScale <= 0 {
		return layout.Dimensions{}
	}
	offsetX := (float64(area.X) - float64(w)*canvasScale) / 2
	offsetY := (float64(area.Y) - float64(h)*canvasScale) / 2
	for _, entry := range origins {
		at := fxProject(view, entry.origin, w, h)
		if at == nil {
			continue
		}
		centreX, centreY := offsetX+at.X*canvasScale, offsetY+at.Y*canvasScale
		paintFXAxesDot(gtx, centreX, centreY, render.ModelMarkerFwdColor)
		for frameIndex, frame := range entry.frames {
			cx := centreX + (float64(frameIndex)-0.5)*fxAxesPairGap
			for axisIndex, axis := range frame {
				probe := fxProject(view, entry.origin.Add(axis.MulScalar(fxAxesProbe)), w, h)
				if probe == nil {
					continue
				}
				dx, dy := (probe.X-at.X)*canvasScale, (probe.Y-at.Y)*canvasScale
				length := math.Hypot(dx, dy)
				if length < 1e-6 {
					continue
				}
				endX, endY := cx+dx/length*fxAxesTriadLen, centreY+dy/length*fxAxesTriadLen
				col := [3][4]float64{render.ModelMarkerXColor, render.ModelMarkerYColor, render.ModelMarkerZColor}[axisIndex]
				paintFXAxesLine(gtx, cx, centreY, endX, endY, col)
				paintFXAxesText(gtx, th, [3]string{"X", "Y", "Z"}[axisIndex], endX-3, endY-7, col)
			}
			label := "local"
			if frameIndex == 1 {
				label = "world"
			}
			paintFXAxesText(gtx, th, label, cx-15, centreY+11, render.ModelMarkerFwdColor)
		}
	}
	return layout.Dimensions{Size: area}
}

func paintFXAxesLine(gtx layout.Context, ax, ay, bx, by float64, col [4]float64) {
	r := image.Rect(int(math.Min(ax, bx)), int(math.Min(ay, by)), int(math.Max(ax, bx))+1, int(math.Max(ay, by))+1)
	if r.Dx() < 2 {
		r.Min.X--
		r.Max.X = r.Min.X + 2
	}
	if r.Dy() < 2 {
		r.Min.Y--
		r.Max.Y = r.Min.Y + 2
	}
	a := clip.Rect(r).Push(gtx.Ops)
	paint.ColorOp{Color: fxAxesColor(col)}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	a.Pop()
}

func paintFXAxesDot(gtx layout.Context, x, y float64, col [4]float64) {
	r := image.Rect(int(math.Round(x))-2, int(math.Round(y))-2, int(math.Round(x))+3, int(math.Round(y))+3)
	a := clip.Rect(r).Push(gtx.Ops)
	paint.ColorOp{Color: fxAxesColor(col)}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	a.Pop()
}

func paintFXAxesText(gtx layout.Context, th *material.Theme, text string, x, y float64, col [4]float64) {
	l := material.Label(th, unit.Sp(9), text)
	l.Color = fxAxesColor(col)
	l.MaxLines = 1
	measure := gtx
	measure.Constraints.Min = image.Point{}
	measure.Constraints.Max = image.Pt(128, 32)
	d := l.Layout(measure)
	if d.Size.X <= 0 || d.Size.Y <= 0 {
		return
	}
	place := gtx
	place.Constraints.Min = image.Point{}
	place.Constraints.Max = d.Size
	tr := op.Offset(image.Pt(int(math.Round(x)), int(math.Round(y)))).Push(place.Ops)
	l.Layout(place)
	tr.Pop()
}

func fxAxesColor(c [4]float64) color.NRGBA {
	to8 := func(v float64) uint8 {
		if v <= 0 {
			return 0
		}
		if v >= 1 {
			return 255
		}
		return uint8(v*255 + 0.5)
	}
	return color.NRGBA{R: to8(c[0]), G: to8(c[1]), B: to8(c[2]), A: to8(c[3])}
}

// ─── merged from fx_markers.go ───────────────────────────────────────────────

// modelMarkerForwards remains for existing same-package diagnostics; the
// cached marker-frame analysis is implemented by internal/whm.Model.
func modelMarkerForwards(m *WhmModel) []int {
	if m == nil {
		return nil
	}
	return whm.ModelMarkerForwards(&m.Model)
}
