package app

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"log"
	"math"
	"path/filepath"
	"strings"
	"time"

	"dow-mod-studio/internal/3d_engine"
	"dow-mod-studio/internal/3d_viewer"
	"dow-mod-studio/internal/app/render"
	"dow-mod-studio/internal/fx"
	"dow-mod-studio/internal/sgb"
	"dow-mod-studio/internal/whe"
	"dow-mod-studio/internal/whm"
	"gioui.org/io/event"
	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

// modelMaterialResult carries resolved textures back to the UI goroutine.
// modelFrame is one finished software render and the serial it was asked for.
type modelFrame struct {
	image  *image.RGBA
	serial uint64
}

// modelMaterialResult carries resolved textures back to the UI goroutine.
type modelMaterialResult struct {
	materials   map[string]render.WhmMaterial
	rsh         map[string][]byte
	aliases     map[string]string
	diagnostics map[string]modelMaterialDiagnostic
	missing     int
	// more says another result follows on the same channel. A map scene
	// delivers its terrain diffuse first and its entities second, so the
	// channel stays set until a result arrives without one following it.
	more bool
	// model replaces the viewer's model outright: how a map scene grows
	// from the terrain-only mesh it opens with into the full scene with
	// the placed objects. A .whm tab never sends one.
	model *WhmModel
	// entityMeshes maps the scene's uniquely named entity meshes back to the
	// stable scenario IDs used by the map tab's in-memory selection.
	entityMeshes     map[string]int
	animatedEntities []sgb.AnimatedEntity
}

// modelViewer is the per tab state of the 3D preview. Everything here is
// touched on the UI goroutine only; workers communicate through channels.
type modelViewer struct {
	Model                *WhmModel
	Materials            map[string]render.WhmMaterial
	MaterialAliases      map[string]string
	Anim                 *WhmAnimation
	Anims                []*WhmAnimation
	AnimPlaybackDuration float64
	AnimatedEntities     []sgb.AnimatedEntity
	SgbAnimLast          time.Time
	// poseCache is the pose at poseTime of poseAnim, kept so a frame that
	// asks for the pose more than once walks the skeleton once.
	PoseCache          WhmPose
	PoseAnim           *WhmAnimation
	PoseTime           float32
	PoseValid          bool
	AnimIndex          int
	Playing            bool
	Time               float32 // normalised 0..1 within the clip
	ShowSkeleton       bool
	ShowSelection      bool
	SelectionIndicator *engine.SelectionIndicator
	Textured           bool
	// frame is the last software render, painted by the canvas.
	Frame *image.RGBA
	// frameCh delivers the render in flight; serial orders renders so a late
	// frame never replaces a newer one, and pending records a camera move that
	// arrived while a render was running.
	FrameCh chan modelFrame
	Serial  uint64
	Pending bool
	// painted is the serial of the last software frame the canvas scheduled a
	// paint for, so a frame that lands between layouts gets one.
	Painted uint64
	// rendering is set while a frame is being produced.
	Rendering bool
	// gpuBroken and gpuFrame belong to the hardware path. gpuFrame is the last
	// frame drawn on the GPU; it lives in video memory, so painting it is a
	// hand over of a device handle rather than a copy. gpuRetries counts
	// renders deferred by a transient backend refusal (see modelGpuErrTransient):
	// past modelGpuRetryLimit the hardware path is given up on as if it had
	// failed outright.
	GpuBroken  bool
	GpuFrame   render.Frame
	GpuRetries int
	// gpuProbeW/H and gpuProbedTextured record which GPU frames have already
	// been read back, so the readback runs on the transitions that matter only.
	gpuProbeW, gpuProbeH int
	GpuProbedTextured    bool
	// rshData holds raw .rsh bytes for the Vulkan block-compressed path.
	RshData map[string][]byte
	// materialDiagnostics records where each map came from and which image
	// encoding decoded successfully, keyed by the model's material reference.
	MaterialDiagnostics map[string]modelMaterialDiagnostic
	// sideControls puts the playback strip beside the canvas instead of under
	// it, which is what the object window's viewport wants: its canvas is a
	// third of the window wide and a strip of eight buttons does not fit under
	// it.
	SideControls bool

	viewer.CameraState
	// uv is the mapping every material is drawn with, and uvMaterials holds a
	// mapping of its own for the ones that need a different one. It is a
	// reading of the model rather than a setting of the app: a map can be
	// authored the wrong way round, and nothing in the file says so.
	Uv          render.UVMapping
	UvMaterials map[string]*render.UVMapping
	// uvSelection names the material the mapping controls are editing, and is
	// empty for the one they all share. uvSelectionDirty says the controls
	// still show the previous selection's numbers, which is what stops the
	// sliders being rewritten from a mapping before the reader has been shown
	// which one they are now looking at. uvTextureClicks are the buttons of
	// the texture list, sized as the panel is laid out.
	UvSelection      string
	UvSelectionDirty bool
	UvTextureClicks  []widget.Clickable
	// The three sliders are the widget's own 0..1 value rather than a float
	// the panel and the renderer both write to, so there is no second copy to
	// fall out of step; the real numbers are read out of them by uvPull.
	uvU, uvV, uvScale widget.Float
	// The rest of the mapping. The three sliders are the widget's own 0..1
	// value rather than a float the panel and the renderer both write to, so
	// there is no second copy to fall out of step; the real numbers are read
	// out of them by render.UVMapping.
	// uvExtent is the range of the model's own coordinates, measured once when
	// the model is opened: [minU, minV, maxU, maxV], with uvExtentOK false
	// for a model with no vertices. It is read out beside the mapping rather
	// than applied to it, so that the numbers a reader is given are the ones
	// the file actually holds.
	UvExtent   [4]float32
	UvExtentOK bool

	// Async material loading and viewport state.
	Loading         bool
	MaterialsLoaded bool
	Missing         int
	RenderW         int
	RenderH         int
	Stop            context.CancelFunc

	// showFX plays the model's effects over the frame. They live in the
	// events its entity blueprint references and hang off markers on its
	// bones, so they are resolved the first time the button is pressed and
	// simulated from then on, which is what keeps the canvas repainting
	// while the toggle is on.
	ShowFX      bool
	ShowFXAxes  bool
	ShowGround  bool
	GroundImage *image.RGBA
	FxEffects   []fxOverlayEffect
	FxAxesClick widget.Clickable
	FxTextures  map[string]*image.RGBA
	// FX checklist selection is reset with the clip, then defaults to the
	// events on that clip's timelines.
	FxSelectionAnim   string
	FxManualOn        map[int]bool
	FxManualOff       map[int]bool
	FxOverrides       map[int]fxAttributeOverride
	FxAttrClicks      []widget.Clickable
	FxAttrIndex       int
	FxAttrClick       widget.Clickable
	FxAttrOpen        bool
	FxAttrDragging    bool
	FxAttrDragX       float32
	FxAttrDragY       float32
	FxAttrWindowX     int
	FxAttrWindowY     int
	FxAttrPosX        widget.Editor
	FxAttrPosY        widget.Editor
	FxAttrPosZ        widget.Editor
	FxAttrRotX        widget.Editor
	FxAttrRotY        widget.Editor
	FxAttrRotZ        widget.Editor
	FxAttrSetPos      widget.Clickable
	FxAttrSetAngle    widget.Clickable
	FxAttrResetPos    widget.Clickable
	FxAttrResetRot    widget.Clickable
	FxAttrError       string
	FxAttrSetDistance widget.Clickable
	FxAttrDir         widget.Clickable
	FxAttrMarker      widget.Clickable
	FxAttrClose       widget.Clickable
	FxAttrTitle       widget.Clickable
	FxAttrDistance    widget.Editor
	FxAttrDragTag     event.Tag
	FxAttrDragOrigin  image.Point
	// fxScale uniformly adjusts model-local FX geometry while leaving marker
	// origins fixed. One is the authored model-space size; the viewport exposes
	// the factor to inspect effects whose sizes need manual calibration.
	FxScale float64
	// fxRotX, fxRotY and fxRotZ turn every effect about its own marker, in
	// degrees, around the marker's own axes rather than the model's: X/Y/Z are
	// the three columns of the marker's frame, so a beam turns with the bone
	// it hangs off instead of with the world it happens to sit in. The marker
	// itself does not move — only what is measured out from it. X lifts and
	// drops an effect, Y swings it side to side, Z spins it around the
	// direction it already faces. They are a dial, not a feature, and exist
	// because a marker's stored forward axis is not always the way the effect
	// actually points — which column of the marker's matrix is "forwards" is a
	// question the file answers in a way that is quicker to measure against the
	// picture than to read out of a thousand lines of it. Zero leaves every
	// effect as authored.
	fxRotX, fxRotY, fxRotZ float64
	// fxSound is the model's declared audio: what plays when the clip on screen
	// reaches the moments its timeline names. It is built from the same .whe the
	// effects are read out of, and is nil for a model that declares no sounds.
	FxSound *fx.SoundPlayer
	// fxSoundOn is what the sound button shows: whether the sounds play. It is
	// separate from the player's own flag so that switching them off and on
	// again does not decode anything a second time.
	FxSoundOn bool
	// fxOverlay is the software-rendered particle image and fxScene is its
	// Vulkan rendering input.
	FxOverlay *image.RGBA
	FxScene   *fxGpuScene
	FxCh      chan fxLoadResult
	// fxSoundCh carries the sound half of the same work. It is separate from
	// fxCh because sounds are wanted from the first frame on screen while the
	// effects wait for the FX button, and a worker that had to wait for both
	// would leave the audio waiting on a toggle nobody pressed.
	FxSoundCh chan fxLoadResult
	FxErr     string
	FxLast    time.Time
	// fxAnim is the clip the effects were last stepped for. A model with
	// timelines only plays the events its own clip fires, so a change of clip
	// is what tells the simulations to start again.
	FxAnim string
	// fxClipPrev is where the playhead was on the previous frame, in seconds
	// into the clip, and fxClipPrevAnim the clip it belonged to. The effects
	// are stepped by the difference between the two rather than by wall clock
	// time, which is what makes an event start when its clip reaches the
	// moment the timeline names instead of when the clip began.
	FxClipPrev     float64
	FxClipPrevAnim string
	// fxTimelines are the motion timelines the worker's parse produced. They are
	// kept on the viewer rather than only inside the sound player because the
	// scrub bar reads them too: they are the only place that says which frames
	// of a clip something fires on.
	FxTimelines []whe.FxTimeline
	// scrubRect is where the frame strip was drawn last frame, so a press inside
	// it winds the playhead instead of starting a camera drag. It is empty when
	// there is nothing to draw.
	ScrubRect image.Rectangle

	// markerIndex is the marker the markers list has picked, or -1 for none.
	// The reader uses it to find one attachment point on a model that
	// carries a dozen, which is the question a model file answers worst.
	MarkerIndex int
	// boneIndex is the bone the bones list has picked, or -1 for none. The
	// overlay draws it in the pick colour and the panel shows its
	// parameters; it is separate from markerIndex because a bone and a
	// marker are picked out of different lists.
	BoneIndex int

	// boneKeyIndex and boneKeyMode describe the bone keyframe the user is
	// currently editing, or -1/-1 when nothing is picked out of the bone's
	// keyframes. boneKeyIndex counts position keys first, then rotation keys,
	// so the same index space is shared by PosKey and RotKey.
	BoneKeyIndex int
	BoneKeyMode  int

	// modelFile is the tree path of the .whm being shown, which is where its
	// own textures are looked for first: a model and the maps it uses sit
	// together far more often than not.
	ModelFile string

	Tag      event.Tag
	Dragging bool
	Panning  bool
	// seeking says a press landed on the frame strip rather than on the model,
	// so the drag that follows is a scrub and not a camera turn.
	Seeking bool
	// keyDrag says the press that started the seek landed on the picked bone's
	// keymark, so the drag that follows carries that key along the strip
	// instead of scrubbing the playhead. keyDragMoved says one of the drag's
	// steps actually put the key somewhere new — the step that makes the edit
	// worth marking dirty and reporting when the drag ends.
	KeyDrag      bool
	KeyDragMoved bool
	LastX        float32
	LastY        float32
	LastFrame    time.Time
	Redraw       bool
}

// newModelViewer prepares a viewer for a decoded model. The path is the one the
// model was read from, which the texture loader uses to find the maps that sit
// beside it.
func newModelViewer(model *WhmModel, modelFile string) *modelViewer {
	camera := render.NewCameraState(&model.Model, 0.7, 0.2, render.DefaultFov)
	return &modelViewer{
		Model:     model,
		ModelFile: modelFile,
		Materials: map[string]render.WhmMaterial{},
		RshData:   map[string][]byte{},
		// Textures start on. Set here rather than when a texture batch
		// lands, so a Textures toggle made while loading is not overwritten.
		Textured:    true,
		CameraState: camera,
		// The map as the file has it, and the sliders showing that: no
		// offset, no turn, no mirror, and a scale of one. A reader moves
		// them away from that and back to it.
		Uv:      render.UVIdentityMapping(),
		uvU:     widget.Float{Value: 0.5},
		uvV:     widget.Float{Value: 0.5},
		uvScale: widget.Float{Value: (1 - modelUVMinScale) / (modelUVMaxScale - modelUVMinScale)},
		// The model's own coordinates, measured once so that the panel can
		// say how far they run without walking the vertices every frame.
		UvExtent:   render.UVExtentOf(&model.Model),
		UvExtentOK: render.ModelHasUVs(&model.Model),
		// The effects at the size their files ask for; the panel moves this
		// from there.
		FxScale:     fxScaleDefaultValue,
		FxAttrIndex: -1,
		// Nothing has been rendered yet, so the canvas must ask for the first
		// frame as soon as it is laid out.
		Redraw: true,
		// Nothing is picked out of the markers list yet. -1 rather than the
		// zero value, which is marker zero: a reader who never opens the
		// list would otherwise find a red ring on the model's first
		// attachment point with nothing in the panel to explain it.
		MarkerIndex: -1, // Nothing is picked out of the bones list yet, for the same
		// reason: a picked bone with no list on show would be an amber
		// cross on the model and nothing in the panel to explain it.
		BoneIndex:    -1,
		BoneKeyIndex: -1,
		BoneKeyMode:  -1,
	}
}

// newObjectModelViewer creates a WHM preview with the same defaults used by
// the Object window's Viewport: its terrain plane, a useful initial locomotion
// or idle clip, and playback started. Both entry points still share the same
// modelViewer renderer and controls.
func newObjectModelViewer(model *WhmModel, modelFile string, ground image.Image) *modelViewer {
	v := newModelViewer(model, modelFile)
	if ground != nil {
		b := ground.Bounds()
		v.GroundImage = image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
		draw.Draw(v.GroundImage, v.GroundImage.Bounds(), ground, b.Min, draw.Src)
	}
	v.ShowGround = v.GroundImage != nil
	v.SelectAnim(unitDefaultAnim(model))
	v.Playing = model != nil && len(model.Animations) > 0
	v.LastFrame = time.Time{}
	return v
}

// modelClose stops this viewer's async jobs.
func ModelClose(ui *uiState, v *modelViewer) {
	if v != nil {
		v.Close()
	}
}

// close stops any running render so the goroutine does not outlive the tab.
func (v *modelViewer) Close() {
	if v == nil {
		return
	}
	if v.Stop != nil {
		v.Stop()
		v.Stop = nil
	}
	// A sound outlives the frame that started it, so anything playing has to
	// be released here rather than left to the tab's last picture.
	v.FxSound.Close()
	v.FxSound = nil
	// A worker started for this model may still be decoding. Its answer is
	// dropped rather than applied: the samples it decoded were chosen for a
	// model this viewer no longer shows, and applying them would wire a sound
	// player onto a closed tab. fxStartSoundLoad refuses to start again while
	// the channel is set, so clearing it here lets the next model load afresh.
	if v.FxSoundCh != nil {
		go func(ch chan fxLoadResult) {
			<-ch
		}(v.FxSoundCh)
		v.FxSoundCh = nil
	}
}

// invalidate marks the frame stale, which makes the next layout start a render.
func (v *modelViewer) Invalidate() {
	if v != nil {
		v.Redraw = true
	}
}

// setRenderSize records the size the next GPU render should use. A change means
// the projection changed, so the view needs a redraw.
func (v *modelViewer) SetRenderSize(w, h int) {
	if w < 1 || h < 1 {
		return
	}
	if v.RenderW == w && v.RenderH == h {
		return
	}
	log.Printf("3D preview: render size %dx%d -> %dx%d", v.RenderW, v.RenderH, w, h)
	v.RenderW, v.RenderH = w, h
	v.Redraw = true
}

// requestRender starts a background render unless one is running. Only the
// newest request is kept, so a burst of camera movement collapses into a
// single follow-up frame.
// renderModelOnGpu draws a frame with the hardware renderer, which is far
// cheaper than the software one and can animate. It reports false when the
// software path has to draw instead: no hardware renderer, the skeleton or
// the settings checkbox, or a failure that marks the path broken.
//
// The pass runs here, on the goroutine that calls Frame and before it. Its
// command buffer remains open so the next Gio frame submits both the offscreen
// render and the blit that samples it in queue order.
func RenderModelOnGpu(ui *uiState, gtx layout.Context, t *fileTab, w, h int) bool {
	v := t.Model
	if v == nil {
		return false
	}
	reason := ModelGpuUnavailable(ui, v)
	if reason != "" {
		// The software path takes over, so its frame has to be the one on
		// screen rather than whatever the hardware path drew last.
		if modelGpuSkipLogged != reason {
			log.Printf("3D preview: using software renderer (%s)", reason)
			modelGpuSkipLogged = reason
		}
		v.GpuFrame = nil
		return false
	}
	if modelGpuSkipLogged != "" {
		log.Printf("3D preview: hardware renderer taking over again (%s resolved)", modelGpuSkipLogged)
		modelGpuSkipLogged = ""
	}
	gpu := ModelGpuRenderer(ui)
	opts := v.Options(w, h)
	opts.Quality = render.ModelQualityFinal
	if t.Kind == "sgb" && t.SgbView3D {
		opts.SgbSelectedEntities = t.SgbSelectedEntities
		opts.SgbEntityMeshIDs = t.SgbEntityMeshIDs
	}
	// Textures off means no materials and no .rsh lookups, so every draw
	// falls back to the base colour. The GPU path must honour this too:
	// it used to receive v.Materials unconditionally.
	var mats map[string]render.WhmMaterial
	var rsh func(string) ([]byte, bool)
	if v.Textured {
		mats = v.Materials
		if len(v.RshData) > 0 {
			rsh = func(material string) ([]byte, bool) {
				data, ok := v.RshData[material]
				return data, ok
			}
		}
	}
	// The first frame of a stretch on the hardware path proves the render
	// itself completed; without it a silent window cannot be told apart from
	// a render that never returned.
	firstGPUFrame := v.GpuFrame == nil
	start := time.Now()
	frame, err := gpu.Render(&v.Model.Model, v.Pose(), opts, mats, rsh)
	if err == nil {
		d := time.Since(start)
		if firstGPUFrame {
			log.Printf("3D preview: GPU frame ready %dx%d in %v (serial=%d)", w, h, d, v.Serial)
		}
		if d >= 50*time.Millisecond {
			log.Printf("3D preview: slow GPU render %v (%dx%d)", d, w, h)
		}
		// Read the frame back on the transitions that matter (the first frame,
		// the first with textures bound, and the first after a resize) so a
		// grey window can be told apart from a black model and lost draws.
		needProbe := v.gpuProbeW != w || v.gpuProbeH != h
		if rsh != nil && !v.GpuProbedTextured {
			needProbe = true
			v.GpuProbedTextured = true
		}
		if needProbe {
			v.gpuProbeW, v.gpuProbeH = w, h
			context := fmt.Sprintf("%dx%d textures=%v", w, h, rsh != nil)
			if !render.ProbeFrame(frame, context) {
				// The device did not survive the readback. Every later submit
				// would fail the same way, so the hardware path is given up on
				// here rather than one frame at a time.
				log.Printf("3D preview: hardware render gave up after the probe, falling back to software")
				v.GpuBroken = true
				v.GpuFrame = nil
				return false
			}
		}
	}
	if err != nil {
		if modelGpuErrTransient(err) {
			// The backend refusing a render right now, not a device that has
			// failed: the custom pass's command buffer stays open until Gio
			// submits the frame it belongs to, and a render asked for before
			// that is refused. The .whm preview starts playback in its own
			// constructor, so its second render lands in exactly that window
			// — marking the path broken here stranded it on the software
			// renderer for the rest of the session. Ask for another frame
			// instead, bounded so a backend that never clears still gives up.
			if v.GpuRetries < modelGpuRetryLimit {
				v.GpuRetries++
				v.Redraw = true
				// The counter is reset by every successful render, so the
				// modulo above never fires while the path keeps working;
				// a time throttle is what keeps the log readable.
				if time.Since(modelGpuRetryLogged) >= time.Second {
					modelGpuRetryLogged = time.Now()
					log.Printf("3D preview: GPU render deferred (%v), retry %d/%d", err, v.GpuRetries, modelGpuRetryLimit)
				}
				return v.GpuFrame != nil
			}
			log.Printf("3D preview: hardware render still refused after %d deferred retries, falling back to software: %v", v.GpuRetries, err)
		} else {
			// One failure is enough: the software path is always there.
			log.Printf("3D preview: hardware render failed, falling back to software: %v", err)
		}
		v.GpuBroken = true
		v.GpuFrame = nil
		return false
	}
	v.GpuRetries = 0
	v.GpuFrame = frame
	return true
}

// modelGpuRetryLimit bounds how many renders a transient refusal may defer
// before the hardware path is treated as broken: 300 frames at the 16ms the
// canvas invalidates on is about five seconds of a refusal that never clears.
const modelGpuRetryLimit = 300

// modelGpuRetryLogged throttles the deferral line to once a second.
var modelGpuRetryLogged time.Time

// modelGpuErrTransient reports whether an error from the hardware renderer is
// a backend refusal to run right now rather than a device failure. The Vulkan
// backend of the Gio fork refuses a custom pass, or a texture upload, while
// Gio is mid-frame — that is, while the previous custom pass's command buffer
// is still open awaiting the frame that submits it. Such a refusal clears by
// itself once that frame goes through, so the render is worth asking for again.
func modelGpuErrTransient(err error) bool {
	s := err.Error()
	return strings.Contains(s, "cannot run inside a Gio frame") ||
		strings.Contains(s, "a custom pass is already active") ||
		strings.Contains(s, "command buffer is in flight") ||
		strings.Contains(s, "upload while a custom pass is active") ||
		strings.Contains(s, "another command buffer is already being recorded")
}

func (v *modelViewer) RequestRender(width, height, quality int) {
	if v.Rendering {
		v.Pending = true
		return
	}
	opts := v.Options(width, height)
	opts.Quality = quality
	model := v.Model
	pose := v.Pose()
	v.Serial++
	serial := v.Serial
	ch := make(chan modelFrame, 1)
	v.FrameCh = ch
	v.Rendering = true
	go func() {
		ch <- modelFrame{image: render.RenderWhmModel(&model.Model, pose, opts), serial: serial}
	}()
}

// modelData returns the decoded model the canvas renders, or nil before one is loaded.
func (v *modelViewer) ModelData() *whm.Model {
	if v.Model == nil {
		return nil
	}
	return &v.Model.Model
}

// selectionIndicatorIfShown returns DATASEUI geometry only while its preview
// toggle is on; the settings themselves remain editable while it is off.
func (v *modelViewer) SelectionIndicatorIfShown() *engine.SelectionIndicator {
	if v == nil || !v.ShowSelection || v.SelectionIndicator == nil {
		return nil
	}
	return v.SelectionIndicator
}

// options builds the render request for the current camera and pose.
func (v *modelViewer) Options(width, height int) render.RenderOptions {
	var mats map[string]render.WhmMaterial
	if v.Textured {
		mats = v.Materials
	}
	return render.RenderOptions{
		RenderOptions: engine.RenderOptions{
			Width:    width,
			Height:   height,
			Yaw:      v.Yaw,
			Pitch:    v.Pitch,
			Distance: v.Distance,
			Target:   v.Center,
			Fov:      v.FOV,
			// Both terms are fractions of full brightness, and the preview is
			// deliberately drawn flat: past a whole one the shader's
			// ambient + diffuse * lambert is clamped to 1, so the model shows
			// its texture at its own brightness with no shading over it, which
			// is what reading a model's colours in a preview wants. The
			// particles take the same light (see fxSceneLight), so the effects
			// are drawn under exactly this setting too rather than under one
			// of their own.
			Ambient:   10,
			Diffuse:   10,
			BaseColor: render.RgbaWhite,
			Skeleton:  v.ShowSkeleton,
			// nil rather than v.MarkerIndex: the viewer's -1 is its own
			// "nothing picked", and the render request says the same thing
			// by carrying no marker at all.
			Marker:        render.PickedMarker(v.MarkerIndex),
			PickedBone:    render.PickedBone(v.BoneIndex),
			UVDefault:     v.Uv,
			UVPerMaterial: v.UvMaterials, MeshAlpha: v.MeshAlphas(),
			AnimatedEntities: v.AnimatedEntities,
		},
		MaterialOverrides:  mats,
		MaterialAliases:    v.MaterialAliases,
		AnimatedEntities:   v.AnimatedEntities,
		GroundImage:        v.GroundImage,
		Ground:             v.ShowGround && v.GroundImage != nil,
		FX:                 v.FxScene,
		SelectionIndicator: v.SelectionIndicatorIfShown(),
	}
}

// pollModelJobs runs on the UI goroutine and applies texture/FX results.
func PollModelJobs(ui *uiState) {
	for _, t := range ui.OpenTabs {
		PollViewerJobs(ui, t)
		PollSgbObjectModel(ui, t)
		PollSgbObjectMaterials(ui, t)
		if t.SgbPlacementPending != nil && t.Model != nil && !t.Model.Loading && !t.Model.Rendering && t.Model.FrameCh == nil && t.ModelMaterialCh == nil && t.SgbObjectModelCh == nil && t.SgbObjectMaterialsCh == nil && len(t.SgbObjectMaterialsPending) == 0 && t.SgbObjectModel != nil {
			request := *t.SgbPlacementPending
			t.SgbPlacementPending = nil
			AppendSgbPlacement(ui, t, request, t.SgbObjectModel)
		}
	}
	// The object window's viewport has a viewer that is not a file tab, and it
	// renders and loads textures exactly the same way.
	PollViewerJobs(ui, ui.UnitModel)
}

// pollViewerJobs takes whatever one viewer has finished: its textures and FX.
// A nil tab, or one with no model on it, is the normal case for most of them.
func PollViewerJobs(ui *uiState, t *fileTab) {
	if t == nil || t.Model == nil {
		return
	}
	v := t.Model
	if t.ModelMaterialCh != nil {
		select {
		case res := <-t.ModelMaterialCh:
			if !res.more {
				t.ModelMaterialCh = nil
			}
			v.Loading = false
			v.MaterialsLoaded = !res.more
			v.Materials = res.materials
			v.MaterialAliases = res.aliases
			if len(res.materials) > 0 {
				v.Textured = true
			}
			log.Printf("3D preview: textures ready: %d materials resolved, %d missing", len(res.materials)+len(res.rsh), res.missing)
			if len(res.rsh) > 0 {
				v.RshData = res.rsh
			}
			if res.diagnostics != nil {
				v.MaterialDiagnostics = res.diagnostics
			}
			if res.missing > 0 {
				v.Missing = res.missing
			}
			// A map scene swaps in the model with its objects; a .whm
			// tab never sends one and keeps its model unchanged.
			if res.model != nil {
				v.Model = res.model
			}
			if res.model != nil {
				t.Model.AnimatedEntities = res.animatedEntities
			}
			if res.model != nil && res.entityMeshes != nil {
				t.SgbEntityMeshIDs = res.entityMeshes
				if t.SgbSelectedEntities == nil {
					t.SgbSelectedEntities = make(map[int]bool)
				}
			}
			v.Invalidate()
		default:
		}
	}
	if v.FxCh != nil {
		select {
		case res := <-v.FxCh:
			v.FxApply(res)
		default:
		}
	}
	if v.FxSoundCh != nil {
		select {
		case res := <-v.FxSoundCh:
			v.FxApplySoundsOnly(res)
		default:
		}
	}
	if v.FrameCh != nil {
		select {
		case frame := <-v.FrameCh:
			v.FrameCh = nil
			v.Rendering = false
			if frame.serial >= v.Serial {
				v.Serial = frame.serial
				v.Frame = frame.image
			} else {
				// A frame the camera already moved past.
				v.Redraw = true
			}
			// A camera move during the render means the frame is already
			// stale; start the next one right away.
			if v.Pending {
				v.Pending = false
				v.Redraw = true
			}
		default:
		}
	}
}

// ─── merged from viewer_controls.go ───────────────────────────────────────────────

// modelControlsWithUV is the strip under a .whm canvas. The mapping controls
// are a dialog of their own, so there is nothing else to draw under the canvas:
// the name is kept because the canvas area is the one place that has to decide
// what goes below itself.
func ModelControlsWithUV(ui *uiState, gtx layout.Context, t *fileTab) layout.Dimensions {
	return ModelControls(ui, gtx, t)
}

// modelControls is the strip under the canvas. A viewer whose controls live
// beside it rather than under it — the object window's viewport, where the
// canvas is a third of the window wide and the strip does not fit — is laid out
// without one.
func ModelControls(ui *uiState, gtx layout.Context, t *fileTab) layout.Dimensions {
	v := t.Model
	ModelControlClicks(ui, gtx, t)
	animCount := len(v.Model.Animations)
	// The strip is two rows tall here, so the one-row clamp is applied to
	// each row below rather than to the whole block: applied outside it
	// would cap the pair at the height of one and clip the second.
	return layout.Inset{Top: unit.Dp(4), Left: unit.Dp(6), Right: unit.Dp(6), Bottom: unit.Dp(6)}.Layout(gtx,
		func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(modelRowHeight(gtx),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return SmallButtonDisabled(ui, gtx, &t.ModelPlay, modelPlayLabel(v), animCount == 0)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return SmallButtonDisabled(ui, gtx, &t.ModelPrevAnim, "◀", animCount == 0)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.Th, unit.Sp(10), modelClipLabel(v))
							l.Color = cTextDim
							l.MaxLines = 1
							return layout.Inset{Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx, l.Layout)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return SmallButtonDisabled(ui, gtx, &t.ModelNextAnim, "▶", animCount == 0)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return SmallButtonDisabled(ui, gtx, &t.ModelStepBack, "|◀", animCount == 0)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return SmallButtonDisabled(ui, gtx, &t.ModelStepFwd, "▶|", animCount == 0)
						}),
						layout.Flexed(1, rowGap()),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return SmallButtonDisabled(ui, gtx, &t.ModelTexture, "Textures", len(v.Materials) == 0)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return SmallButtonDisabled(ui, gtx, &t.ModelSkeleton, "Skeleton", len(v.Model.Bones) == 0)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return SmallButton(ui, gtx, &t.ModelFX, modelFXLabel(v))
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions { return FxAxesButton(ui, gtx, v) }),
						// The ground plane toggle, the same switch the Object
						// Viewport's side panel carries: disabled when the
						// mod carries no ground texture to show.
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return SmallButtonDisabled(ui, gtx, &t.ModelGround, modelGroundLabel(v), v.GroundImage == nil)
						}),
						// The zoom the wheel does, as two steps of it, so the
						// distance can be moved by a known amount without a
						// wheel to hand.
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return SmallButton(ui, gtx, &t.ModelZoomOut, modelZoomOutLabel)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return SmallButton(ui, gtx, &t.ModelZoomIn, modelZoomInLabel)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions { return SmallButton(ui, gtx, &t.ModelReset, "Reset view") }),
					)
				}),
			)
		})
}

// modelInfoTable reports what the 3D preview is showing, which makes it obvious
// whether a blank canvas is a missing texture or a parse problem.
func ModelInfoTable(ui *uiState, gtx layout.Context, t *fileTab) layout.Dimensions {
	if t == nil || t.Model == nil || t.Model.Model == nil {
		return layout.Dimensions{}
	}
	v := t.Model
	tris := 0
	for _, mesh := range v.Model.Meshes {
		tris += len(mesh.Indices) / 3
	}
	frame := "—"
	if v.Frame != nil {
		b := v.Frame.Bounds()
		frame = fmt.Sprintf("%d × %d", b.Dx(), b.Dy())
	}
	state := "idle"
	switch {
	case v.Loading:
		state = "loading textures"
	case v.Rendering:
		state = "rendering"
	case v.Frame != nil:
		state = "ready"
	}
	// Several meshes usually share a map, so the count is over distinct
	// materials. Comparing against the mesh count made a model with four
	// textures look like it was missing twenty three of them.
	unique := map[string]bool{}
	for _, m := range v.Model.Meshes {
		if m.Material != "" {
			unique[m.Material] = true
		}
	}
	textures := fmt.Sprintf("%d/%d", len(v.Materials), len(unique))
	if v.Missing > 0 {
		textures += fmt.Sprintf(" (%d missing)", v.Missing)
	}
	anim := "—"
	if v.Anim != nil {
		anim = v.Anim.Name
	}
	rows := [][2]string{
		{"State", state},
		{"Frame", frame},
		{"Triangles", fmt.Sprintf("%d", tris)},
		{"Animation", anim},
		{"Textures", textures},
	}
	// The camera only gets a row when one of the model's own is in use, since
	// an orbit camera has no name to report.
	if v.ActiveCamera != "" {
		rows = append(rows, [2]string{"Camera", v.ActiveCamera})
	}
	children := make([]layout.FlexChild, 0, len(rows))
	for i, row := range rows {
		i, row := i, row
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return TableRow2(ui, gtx, row[0], row[1], i%2 == 0)
		}))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}

// modelMaterialDiagnosticsTable lists each mesh's material reference, source,
// and decoded image encoding. Repeated materials remain visible per mesh so
// the reader can identify which geometry uses a missing or unexpected map.
func ModelMaterialDiagnosticsTable(ui *uiState, gtx layout.Context, t *fileTab) layout.Dimensions {
	if t == nil || t.Model == nil || t.Model.Model == nil {
		return layout.Dimensions{}
	}
	v := t.Model
	if !v.MaterialsLoaded && !v.Loading {
		return centeredPanelLabel(gtx, ui.Th, "Material lookup has not run", unit.Sp(10), cTextDim)
	}
	if v.Loading && len(v.MaterialDiagnostics) == 0 {
		return centeredPanelLabel(gtx, ui.Th, "Resolving materials…", unit.Sp(10), cTextDim)
	}
	rows := make([]layout.FlexChild, 0, len(v.Model.Meshes)*3)
	for meshIndex, mesh := range v.Model.Meshes {
		if mesh.Material == "" {
			continue
		}
		diag, ok := v.MaterialDiagnostics[mesh.Material]
		if !ok {
			diag = modelMaterialDiagnostic{Format: "—", Status: "not resolved"}
		}
		meshLabel := mesh.Name
		if meshLabel == "" {
			meshLabel = fmt.Sprintf("Mesh %d", meshIndex+1)
		}
		rows = append(rows,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return TableRow2WithTooltip(ui, gtx, meshLabel, mesh.Material, meshIndex%2 == 0, mesh.Material)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return TableRow2WithTooltip(ui, gtx, "Format", diagnosticValue(diag.Format), meshIndex%2 != 0, diag.Status)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				path := diag.ResolvedPath
				if path == "" {
					path = diag.Status
				}
				return TableRow2WithTooltip(ui, gtx, "Source", path, meshIndex%2 == 0, diag.ResolvedPath)
			}),
		)
	}
	if len(rows) == 0 {
		return centeredPanelLabel(gtx, ui.Th, "No meshes reference a material", unit.Sp(10), cTextDim)
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, rows...)
}

func diagnosticValue(value string) string {
	if value == "" {
		return "—"
	}
	return value
}

// modelPlayLabel is what the play button says: the action it will do.
func modelPlayLabel(v *modelViewer) string {
	if v.Playing {
		return "Pause"
	}
	return "Play"
}

// modelClipLabel is the name of the clip on show and where it is in the list.
func modelClipLabel(v *modelViewer) string {
	count := len(v.Model.Animations)
	if count == 0 {
		return "no animations"
	}
	name := "—"
	if v.Anim != nil {
		name = v.Anim.Name
	}
	return fmt.Sprintf("%s  [%d/%d]", name, v.AnimIndex+1, count)
}

// onOff renders a boolean for a log line.
func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

// toggleSound turns the model's own audio on and off. Turning it off stops what
// is playing at once, because a sound that keeps going after it was turned off
// is the one case of this that is actually annoying.
func (v *modelViewer) ToggleSound() {
	if v == nil {
		return
	}
	if v.FxSoundOn {
		v.FxSoundOn = false
		if v.FxSound != nil {
			v.FxSound.StopAll()
		}
		log.Printf("3D preview: sound off")
		return
	}
	v.FxSoundOn = true
	log.Printf("3D preview: sound on")
}

// modelGroundLabel reports whether the unit viewport's ground plane is shown.
func modelGroundLabel(v *modelViewer) string {
	if v == nil || v.GroundImage == nil {
		return "no ground"
	}
	if v.ShowGround {
		return "Ground on"
	}
	return "Ground off"
}

// modelSFXLabel is what the sound button reads: whether the model has sounds
// at all, and whether they are playing.
// modelZoomStep is what one step of the zoom changes the camera distance by:
// a notch of the wheel, or a press of one of the two zoom buttons.
//
// It used to be a two percent step raised to the whole scroll amount, and the
// scroll amount for one notch is wheelNotchDelta, so a click multiplied the
// distance by 1.02^120 — a factor of ten. The model went from framed to inside
// out in one click and back in the next, with nothing in between worth looking
// at. One step is now one readable move, and the whole range between the
// closest and furthest the viewer allows is about a dozen steps either way.
const modelZoomStep = 1.15

// zoomBy multiplies the camera distance, keeping it inside the range the viewer
// allows. A factor above one moves the camera away from the model.
func (v *modelViewer) ZoomBy(factor float64) {
	if v == nil || factor <= 0 || factor == 1 {
		return
	}
	v.CameraState = viewer.ZoomBy(v.CameraState, factor)
	v.Invalidate()
}

// modelZoomByDelta turns a scroll amount into the factor it costs the camera.
// The amount is in notches rather than pixels, so a Windows wheel and a
// trackpad — which reports the same gesture in smaller pieces that add up —
// move the camera by the same amount for the same turn of the wheel.
func modelZoomByDelta(delta float32) float64 {
	return math.Pow(modelZoomStep, -float64(delta)/wheelNotchDelta)
}

// ─── merged from viewer_model_controls.go ───────────────────────────────────────────────

// render.DefaultFov moved to internal/app/render as render.DefaultFov. It matches
// what a .whm camera stores, so a model that has one of its own and is put on
// it does not jump in size.

// Interactive 3D preview for a decoded .whm.
//
// Rendering is a CPU rasterisation (see model3d.go) and costs real time, so a
// frame is produced only when the camera or the animation actually changes, and
// the work happens on a worker goroutine. While the camera is moving the last
// frame stays on screen; the full quality frame replaces it when the movement
// stops.

// hiddenMeshes is the set of mesh names the reader has turned off with the
// eye toggle of the mesh list. It is shared by every viewer so hiding a mesh
// in the object window's viewport also hides it in a .whm tab showing the
// same model. The render options are built on the viewer, which has no handle
// on uiState, so the set lives here rather than on the ui state; names are
// compared in lower case because a model's mesh casing is not guaranteed
// consistent between the mesh list and the clips.
var hiddenMeshes = map[string]bool{}

// setMeshHidden records one mesh as shown or hidden for every viewer.
func setMeshHidden(name string, hidden bool) {
	key := strings.ToLower(name)
	if hidden {
		hiddenMeshes[key] = true
	} else {
		delete(hiddenMeshes, key)
	}
}

// meshHidden says whether the reader has turned the named mesh off.
func meshHidden(name string) bool { return hiddenMeshes[strings.ToLower(name)] }

// modelMeshNames is the model's mesh names, each once, in the order the model
// declares them. A mesh ships as several sub meshes (one per material) that
// share a name, so the list de-dupes by name while keeping the first casing
// — which is the order the eye toggle list wants.
func modelMeshNames(m *WhmModel) []string {
	if m == nil {
		return nil
	}
	seen := make(map[string]bool, len(m.Meshes))
	out := make([]string, 0, len(m.Meshes))
	for _, mesh := range m.Meshes {
		key := strings.ToLower(mesh.Name)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, mesh.Name)
	}
	return out
}

// meshAlphas is how much of each mesh the clip on show draws at the playhead,
// for the clips that hide or fade part of a model: the restored monolith
// holds its pieces back through phase_out, and the Obelisk fades its body in
// over the first frames of build_phasing.
//
// The map is built per frame rather than kept, because a model has a few dozen
// meshes at most and one that says nothing is the common case: MeshAlphas
// returns nil for it, and nil means every mesh is drawn whole.
//
// A mesh the reader has hidden with the eye toggle is forced to 0 here, on top
// of whatever the clip says: both the software and the hardware renderer skip
// a mesh whose alpha is 0, so this is the one place that turns the toggle into
// a mesh that is not drawn.
func (v *modelViewer) MeshAlphas() map[string]float32 {
	if v == nil || v.Model == nil {
		return nil
	}
	var out map[string]float32
	if v.Anim != nil {
		// The playhead is normalised over the clip, which is the measure the
		// visibility keys are written in.
		out = v.Model.MeshAlphas(v.Anim.Name, float32(v.Time))
	}
	if len(hiddenMeshes) == 0 {
		return out
	}
	// Force every hidden mesh of this model to 0 by the mesh's own name,
	// which is how both renderers look it up. A hidden name that belongs to
	// another model simply is not in this loop.
	for i := range v.Model.Meshes {
		name := v.Model.Meshes[i].Name
		if !meshHidden(name) {
			continue
		}
		if out == nil {
			out = map[string]float32{}
		}
		out[name] = 0
	}
	return out
}

// The range the three mapping sliders cover. An offset of a whole tile is
// already past anything a reader would be looking for, and a scale of a fifth
// of the map is a quarter of the model repeated five times over.
const (
	modelUVMinOffset = -1
	modelUVMaxOffset = 1
	modelUVMinScale  = 0.2
	modelUVMaxScale  = 4
)

// modelControlClicks applies whichever button of the strip was pressed. It is
// the one place the presses are read, so a strip laid out in a different shape —
// the viewport's two narrow rows — does exactly the same as the one under a
// canvas.
func ModelControlClicks(ui *uiState, gtx layout.Context, t *fileTab) {
	v := t.Model
	if t.ModelPlay.Clicked(gtx) {
		if v.Anim == nil {
			v.SelectAnim(0)
		}
		v.Playing = !v.Playing
		v.LastFrame = time.Time{}
		v.Invalidate()
	}
	if t.ModelPrevAnim.Clicked(gtx) {
		v.SelectAnim(v.AnimIndex - 1)
	}
	if t.ModelNextAnim.Clicked(gtx) {
		v.SelectAnim(v.AnimIndex + 1)
	}
	if t.ModelStepBack.Clicked(gtx) {
		v.StepFrame(-1)
		PickFrameKeyOnTimeline(ui, v)
	}
	if t.ModelStepFwd.Clicked(gtx) {
		v.StepFrame(1)
		PickFrameKeyOnTimeline(ui, v)
	}
	if t.ModelReset.Clicked(gtx) {
		v.ResetCamera()
	}
	if t.ModelZoomIn.Clicked(gtx) {
		v.ZoomBy(modelZoomStep)
	}
	if t.ModelZoomOut.Clicked(gtx) {
		v.ZoomBy(1 / modelZoomStep)
	}
	if t.ModelSkeleton.Clicked(gtx) {
		v.ShowSkeleton = !v.ShowSkeleton
		log.Printf("3D preview: skeleton overlay %s", onOff(v.ShowSkeleton))
		v.Invalidate()
	}
	if t.ModelFX.Clicked(gtx) {
		v.ToggleFX()
	}
	if t.ModelGround.Clicked(gtx) {
		v.ShowGround = !v.ShowGround
		log.Printf("3D preview: ground %s", onOff(v.ShowGround))
		v.Invalidate()
	}
	if t.ModelTexture.Clicked(gtx) {
		v.Textured = !v.Textured
		log.Printf("3D preview: textures %s", onOff(v.Textured))
		v.Invalidate()
	}
}

// modelRowHeight keeps a strip of buttons one row high. A small button fills
// whatever height it is handed, and a row of them under a canvas would take the
// canvas's height and leave it nothing.
func modelRowHeight(gtx layout.Context) layout.Context {
	if h := gtx.Dp(unit.Dp(unitRowHeightDp)); gtx.Constraints.Max.Y > h {
		gtx.Constraints.Max.Y = h
	}
	gtx.Constraints.Min.Y = 0
	return gtx
}

// The two zoom buttons are labelled with the signs rather than with words: the
// strip they sit in is one row of small buttons, and in the object window's
// side panel there is no room for "Zoom in". They are pressed one at a time
// with "Reset view" beside them, which is what says what they are for.
const (
	modelZoomInLabel  = "+"
	modelZoomOutLabel = "−"
)

// uvTextureLabel is what a texture is called in the list: the shared mapping is
// named as such, and a material is named by its own base name, which is what
// the .whm refers to it by.
func uvTextureLabel(material string) string {
	if material == "" {
		return "All textures"
	}
	name := strings.TrimSuffix(filepath.Base(material), filepath.Ext(material))
	if name == "" || name == "." {
		return material
	}
	return name
}

// uvTextureButton is one entry of the texture list. The pressed look is the one
// the selected rows use, so the choice reads as a choice without a word saying
// so.
func UvTextureButton(ui *uiState, gtx layout.Context, btn *widget.Clickable, label string, on bool) layout.Dimensions {
	bg, bd, text := cSaveBg, cSaveBd, cText
	if on {
		bg, bd, text = cSelectBg, cSelectBd, rgb(0xFFFFFF)
	}
	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return frameMeasured(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8), Top: unit.Dp(3), Bottom: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				l := material.Label(ui.Th, unit.Sp(10), label)
				l.Color = text
				l.MaxLines = 1
				return l.Layout(gtx)
			})
		}, bg, bd, unit.Dp(3))
	})
}

// ensureUVTextureClicks sizes the texture list's click targets. They are
// rebuilt per model, so a list of the wrong length is replaced rather than
// grown into.
func (v *modelViewer) EnsureUVTextureClicks(n int) {
	if len(v.UvTextureClicks) != n {
		v.UvTextureClicks = make([]widget.Clickable, n)
	}
}

// uvWholeTiles reports whether an offset is a whole number of map widths, which
// the map repeating that often makes indistinguishable from no offset at all.
func uvWholeTiles(v float32) bool {
	if v == 0 {
		return false
	}
	n := float64(v)
	return math.Abs(n-math.Round(n)) < 0.0005
}

// modelMaterialsNeedLoad reports whether this viewer has never completed its
// texture lookup and has no worker in progress.
func modelMaterialsNeedLoad(t *fileTab) bool {
	return t != nil && t.Model != nil && t.Model.Model != nil &&
		t.ModelMaterialCh == nil && !t.Model.Loading && !t.Model.MaterialsLoaded
}

// layoutModelViewer draws the canvas plus the playback controls.
func LayoutModelViewer(ui *uiState, gtx layout.Context, t *fileTab) layout.Dimensions {
	v := t.Model
	if v == nil || v.Model == nil {
		return centeredPanelLabel(gtx, ui.Th, "No model", unit.Sp(10), cTextDim)
	}
	if modelMaterialsNeedLoad(t) {
		t.ModelMaterialCh = make(chan modelMaterialResult, 1)
		LoadModelMaterials(ui, v, t.ModelMaterialCh)
	}
	// The canvas is always laid out, even before the first frame arrives: it is
	// the canvas that starts the render, so skipping it here would leave the
	// tab permanently blank.
	if v.SideControls {
		return ModelCanvasWithFxList(ui, gtx, t)
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return ModelCanvasWithFxList(ui, gtx, t) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ModelControlsWithUV(ui, gtx, t) }),
	)
}

// fxListLabel lays out one block of the readout, for layout.Rigid.
func fxListLabel(th *material.Theme, s string, col color.NRGBA) func(gtx layout.Context) layout.Dimensions {
	return func(gtx layout.Context) layout.Dimensions {
		l := material.Label(th, unit.Sp(11), s)
		l.Color = col
		l.MaxLines = 14
		return l.Layout(gtx)
	}
}

// clampFloat64 moved to internal/3d_engine as engine.ClampFloat64.

var _ = widget.Clickable{}

// renderModelOnGpu draws a frame with the hardware renderer, which is a few
// orders of magnitude cheaper than the software one and can animate. It reports
// false when there is no hardware renderer, or when the skeleton overlay is
// asked for, since the bone lines are drawn by the software path.
//
// The pass has to run here, on the goroutine that calls Frame and before it, so
// the device commands are ordered ahead of Gio's own drawing.
// modelGpuSkipLogged is the reason the software path was last used, so the
// log records the switch rather than one line per frame.
var modelGpuSkipLogged string

// modelRenderingLogged throttles the "nothing on screen yet" line to once a
// second, since a stuck preview asks every frame.
var modelRenderingLogged time.Time

// modelGPUPaintSkipLogged marks that the DOW_NO_GPU_PAINT diagnostic has been
// reported once.
var modelGPUPaintSkipLogged time.Time

// modelGpuUnavailable says why the hardware path will not draw this frame, or
// "" when it will.
//
// It is the decision renderModelOnGpu acts on, factored out because two
// places now ask it: the renderer itself, and the FX step, which skips
// building the software picture of the particles when the hardware frame is
// going to draw them instead. Two copies of this list would drift, and the
// drift that matters is the one where the picture was not built while the
// hardware path failed anyway.
func ModelGpuUnavailable(ui *uiState, v *modelViewer) string {
	switch {
	case v == nil || v.Model == nil:
		return "no model"
	case ui.ForceSoftRenderer:
		// The settings checkbox wins over every hardware path, in the
		// textured mode as in the skeleton one.
		return "software rendering enabled in settings"
	case v.GpuBroken:
		return "hardware path marked broken"
	case ModelGpuRenderer(ui) == nil:
		return "no hardware renderer on this window"
	}
	return ""
}

// ─── GPU renderer lifecycle (moved from gpu_model_iface.go) ──────────────────

// modelGpu opens the hardware renderer on first use and returns it, or nil when
// there is none. Gio creates the window's GPU along with the first frame, so
// this cannot be done any earlier.
func ModelGpuRenderer(ui *uiState) render.Handle {
	if ui.ModelGpuTried {
		return ui.ModelGpu
	}
	ui.ModelGpuTried = true
	ui.ModelGpu = render.NewForWindow(ui.Win)
	// A tab can be laid out before the window has drawn its first frame, and
	// Gio only creates the device then. That is not a verdict on the backend,
	// so the next layout asks again instead of falling back for good.
	if ui.ModelGpu == nil && render.TooEarly {
		ui.ModelGpuTried = false
	}
	return ui.ModelGpu
}

func GeometryChangedModelGpu(ui *uiState, model *WhmModel) {
	if model == nil || ui.ModelGpu == nil {
		return
	}
	ui.ModelGpu.GeometryChanged(&model.Model)
}

func ForgetModelGpu(ui *uiState, model *WhmModel) {
	if model == nil || ui.ModelGpu == nil {
		return
	}
	ui.ModelGpu.Forget(&model.Model)
}

// releaseModelGpu frees the hardware renderer and everything it cached.
func ReleaseModelGpu(ui *uiState) {
	for _, t := range ui.OpenTabs {
		if t.Model != nil {
			t.Model.GpuFrame = nil
		}
	}
	// The object window's viewport has a viewer of its own, which is not in
	// openTabs, so its video memory frame has to go back separately.
	if ui.UnitModel != nil && ui.UnitModel.Model != nil {
		ui.UnitModel.Model.GpuFrame = nil
	}
	if ui.ModelGpu != nil {
		ui.ModelGpu.Release()
		ui.ModelGpu = nil
	}
	ui.ModelGpuTried = false
}
