package main

import (
	"context"
	engine "dow-mod-studio/internal/3d_engine"
	viewer "dow-mod-studio/internal/3d_viewer"
	"dow-mod-studio/internal/fx"
	"dow-mod-studio/internal/whe"
	"fmt"
	"image"
	"image/draw"
	"log"
	"time"

	"gioui.org/io/event"
	"gioui.org/layout"
	"gioui.org/widget"
)

// modelFrame is the result of one background render.
type modelFrame struct {
	image  *image.RGBA
	serial uint64
}

// modelMaterialResult carries resolved materials back to the UI goroutine. The
// raw .rsh bytes are only sent when the hardware renderer is in use, since it
// uploads them without decoding anything.
type modelMaterialResult struct {
	materials map[string]WhmMaterial
	rsh       map[string][]byte
	missing   int
}

// modelViewer is the per tab state of the 3D preview. Everything here is
// touched on the UI goroutine only; workers communicate through channels.
type modelViewer struct {
	model     *WhmModel
	materials map[string]WhmMaterial
	anim      *WhmAnimation
	// poseCache is the pose at poseTime of poseAnim, kept so a frame that
	// asks for the pose more than once walks the skeleton once.
	poseCache    WhmPose
	poseAnim     *WhmAnimation
	poseTime     float32
	poseValid    bool
	animIndex    int
	playing      bool
	time         float32 // normalised 0..1 within the clip
	showSkeleton bool
	textured     bool
	// sideControls puts the playback strip beside the canvas instead of under
	// it, which is what the object window's viewport wants: its canvas is a
	// third of the window wide and a strip of eight buttons does not fit under
	// it.
	sideControls bool

	viewer.CameraState
	// uv is the mapping every material is drawn with, and uvMaterials holds a
	// mapping of its own for the ones that need a different one. It is a
	// reading of the model rather than a setting of the app: a map can be
	// authored the wrong way round, and nothing in the file says so.
	uv          uvMapping
	uvMaterials map[string]*uvMapping
	// uvSelection names the material the mapping controls are editing, and is
	// empty for the one they all share. uvSelectionDirty says the controls
	// still show the previous selection's numbers, which is what stops the
	// sliders being rewritten from a mapping before the reader has been shown
	// which one they are now looking at. uvTextureClicks are the buttons of
	// the texture list, sized as the panel is laid out.
	uvSelection      string
	uvSelectionDirty bool
	uvTextureClicks  []widget.Clickable
	// The three sliders are the widget's own 0..1 value rather than a float
	// the panel and the renderer both write to, so there is no second copy to
	// fall out of step; the real numbers are read out of them by uvPull.
	uvU, uvV, uvScale widget.Float
	// The rest of the mapping. The three sliders are the widget's own 0..1
	// value rather than a float the panel and the renderer both write to, so
	// there is no second copy to fall out of step; the real numbers are read
	// out of them by uvMapping.
	// uvExtent is the range of the model's own coordinates, measured once when
	// the model is opened: [minU, minV, maxU, maxV], with uvExtentOK false
	// for a model with no vertices. It is read out beside the mapping rather
	// than applied to it, so that the numbers a reader is given are the ones
	// the file actually holds.
	uvExtent   [4]float32
	uvExtentOK bool

	// rshData holds the raw .rsh bytes of every material, which is what the
	// hardware renderer uploads. The software path decodes them instead and
	// keeps the result in materials.
	rshData map[string][]byte
	// gpuFrame is the last frame drawn on the GPU. It lives in video memory
	// and is owned by the renderer.
	gpuFrame modelGpuFrame
	// gpuBroken remembers that hardware rendering failed, so a broken driver
	// does not cost a failed render on every frame.
	gpuBroken bool

	// async state
	frameCh         chan modelFrame
	frame           *image.RGBA
	rendering       bool
	pending         bool
	serial          uint64
	painted         uint64
	loading         bool
	materialsLoaded bool
	missing         int
	renderW         int
	renderH         int
	// gpuProbeW/H mark the frame size the content probe last ran at, and
	// gpuProbedTextured whether a frame with real textures bound was probed.
	gpuProbeW         int
	gpuProbeH         int
	gpuProbedTextured bool
	stop              context.CancelFunc

	// showFX plays the model's effects over the frame. They live in the
	// events its entity blueprint references and hang off markers on its
	// bones, so they are resolved the first time the button is pressed and
	// simulated from then on, which is what keeps the canvas repainting
	// while the toggle is on.
	showFX      bool
	showGround  bool
	groundImage *image.RGBA
	fxEffects   []fxOverlayEffect
	fxTextures  map[string]*image.RGBA
	// fxScale uniformly adjusts model-local FX geometry while leaving marker
	// origins fixed. One is the authored model-space size; the viewport exposes
	// the factor to inspect effects whose sizes need manual calibration.
	fxScale float64
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
	fxSound *fx.SoundPlayer
	// fxSoundOn is what the sound button shows: whether the sounds play. It is
	// separate from the player's own flag so that switching them off and on
	// again does not decode anything a second time.
	fxSoundOn bool
	// fxOverlay is the picture the software path paints over the frame, and
	// fxScene the same particles as the hardware renderer draws them.
	fxOverlay *image.RGBA
	fxScene   *fxGpuScene
	fxCh      chan fxLoadResult
	// fxSoundCh carries the sound half of the same work. It is separate from
	// fxCh because sounds are wanted from the first frame on screen while the
	// effects wait for the FX button, and a worker that had to wait for both
	// would leave the audio waiting on a toggle nobody pressed.
	fxSoundCh chan fxLoadResult
	fxErr     string
	fxLast    time.Time
	// fxAnim is the clip the effects were last stepped for. A model with
	// timelines only plays the events its own clip fires, so a change of clip
	// is what tells the simulations to start again.
	fxAnim string
	// fxClipPrev is where the playhead was on the previous frame, in seconds
	// into the clip, and fxClipPrevAnim the clip it belonged to. The effects
	// are stepped by the difference between the two rather than by wall clock
	// time, which is what makes an event start when its clip reaches the
	// moment the timeline names instead of when the clip began.
	fxClipPrev     float64
	fxClipPrevAnim string
	// fxTimelines are the motion timelines the worker's parse produced. They are
	// kept on the viewer rather than only inside the sound player because the
	// scrub bar reads them too: they are the only place that says which frames
	// of a clip something fires on.
	fxTimelines []whe.FxTimeline
	// scrubRect is where the frame strip was drawn last frame, so a press inside
	// it winds the playhead instead of starting a camera drag. It is empty when
	// there is nothing to draw.
	scrubRect image.Rectangle

	// markerIndex is the marker the markers list has picked, or -1 for none.
	// The reader uses it to find one attachment point on a model that
	// carries a dozen, which is the question a model file answers worst.
	markerIndex int
	// boneIndex is the bone the bones list has picked, or -1 for none. The
	// overlay draws it in the pick colour and the panel shows its
	// parameters; it is separate from markerIndex because a bone and a
	// marker are picked out of different lists.
	boneIndex int

	// boneKeyIndex and boneKeyMode describe the bone keyframe the user is
	// currently editing, or -1/-1 when nothing is picked out of the bone's
	// keyframes. boneKeyIndex counts position keys first, then rotation keys,
	// so the same index space is shared by PosKey and RotKey.
	boneKeyIndex int
	boneKeyMode  int

	// modelFile is the tree path of the .whm being shown, which is where its
	// own textures are looked for first: a model and the maps it uses sit
	// together far more often than not.
	modelFile string

	tag      event.Tag
	dragging bool
	panning  bool
	// seeking says a press landed on the frame strip rather than on the model,
	// so the drag that follows is a scrub and not a camera turn.
	seeking bool
	// keyDrag says the press that started the seek landed on the picked bone's
	// keymark, so the drag that follows carries that key along the strip
	// instead of scrubbing the playhead. keyDragMoved says one of the drag's
	// steps actually put the key somewhere new — the step that makes the edit
	// worth marking dirty and reporting when the drag ends.
	keyDrag      bool
	keyDragMoved bool
	lastX        float32
	lastY        float32
	lastFrame    time.Time
	redraw       bool
}

// newModelViewer prepares a viewer for a decoded model. The path is the one the
// model was read from, which the texture loader uses to find the maps that sit
// beside it.
func newModelViewer(model *WhmModel, modelFile string) *modelViewer {
	camera := NewCameraState(model, 0.7, 0.2, modelDefaultFov)
	return &modelViewer{
		model:       model,
		modelFile:   modelFile,
		materials:   map[string]WhmMaterial{},
		rshData:     map[string][]byte{},
		CameraState: camera,
		// The map as the file has it, and the sliders showing that: no
		// offset, no turn, no mirror, and a scale of one. A reader moves
		// them away from that and back to it.
		uv:      uvIdentityMapping(),
		uvU:     widget.Float{Value: 0.5},
		uvV:     widget.Float{Value: 0.5},
		uvScale: widget.Float{Value: (1 - modelUVMinScale) / (modelUVMaxScale - modelUVMinScale)},
		// The model's own coordinates, measured once so that the panel can
		// say how far they run without walking the vertices every frame.
		uvExtent:   uvExtentOf(model),
		uvExtentOK: modelHasUVs(model),
		// The effects at the size their files ask for; the panel moves this
		// from there.
		fxScale: fxScaleDefaultValue,
		// Nothing has been rendered yet, so the canvas must ask for the first
		// frame as soon as it is laid out.
		redraw: true,
		// Nothing is picked out of the markers list yet. -1 rather than the
		// zero value, which is marker zero: a reader who never opens the
		// list would otherwise find a red ring on the model's first
		// attachment point with nothing in the panel to explain it.
		markerIndex: -1, // Nothing is picked out of the bones list yet, for the same
		// reason: a picked bone with no list on show would be an amber
		// cross on the model and nothing in the panel to explain it.
		boneIndex:    -1,
		boneKeyIndex: -1,
		boneKeyMode:  -1,
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
		v.groundImage = image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
		draw.Draw(v.groundImage, v.groundImage.Bounds(), ground, b.Min, draw.Src)
	}
	v.showGround = v.groundImage != nil
	v.selectAnim(unitDefaultAnim(model))
	v.playing = model != nil && len(model.Animations) > 0
	v.lastFrame = time.Time{}
	return v
}

// modelClose drops this viewer's cached GPU data before stopping its async jobs.
func (ui *uiState) modelClose(v *modelViewer) {
	if v == nil {
		return
	}
	if ui.modelGpu != nil && v.model != nil {
		ui.modelGpu.forget(v.model)
	}
	v.close()
}

// close stops any running render so the goroutine does not outlive the tab.
func (v *modelViewer) close() {
	if v == nil {
		return
	}
	if v.stop != nil {
		v.stop()
		v.stop = nil
	}
	// A sound outlives the frame that started it, so anything playing has to
	// be released here rather than left to the tab's last picture.
	v.fxSound.Close()
	v.fxSound = nil
	// A worker started for this model may still be decoding. Its answer is
	// dropped rather than applied: the samples it decoded were chosen for a
	// model this viewer no longer shows, and applying them would wire a sound
	// player onto a closed tab. fxStartSoundLoad refuses to start again while
	// the channel is set, so clearing it here lets the next model load afresh.
	if v.fxSoundCh != nil {
		go func(ch chan fxLoadResult) {
			<-ch
		}(v.fxSoundCh)
		v.fxSoundCh = nil
	}
}

// invalidate marks the frame stale, which makes the next layout start a render.
func (v *modelViewer) invalidate() {
	if v != nil {
		v.redraw = true
	}
}

// setRenderSize records the size the next render should use. A change means the
// projection changed, so the frame already on screen no longer fits and a
// redraw is scheduled. The first call always schedules one, because that is
// what gets the very first frame rendered.
func (v *modelViewer) setRenderSize(w, h int) {
	if w < 1 || h < 1 {
		return
	}
	if v.renderW == w && v.renderH == h {
		return
	}
	log.Printf("3D preview: render size %dx%d -> %dx%d", v.renderW, v.renderH, w, h)
	v.renderW, v.renderH = w, h
	v.redraw = true
}

// requestRender starts a background render unless one is running. Only the
// newest request is kept, so a burst of camera movement collapses into a
// single follow-up frame.
func (v *modelViewer) requestRender(width, height, quality int) {
	if v.rendering {
		v.pending = true
		return
	}
	log.Printf("3D preview: soft render start serial=%d size=%dx%d", v.serial+1, width, height)
	opts := v.options(width, height, quality)
	model := v.model
	pose := v.pose()
	v.serial++
	serial := v.serial
	ch := make(chan modelFrame, 1)
	v.frameCh = ch
	v.rendering = true
	go func() {
		ch <- modelFrame{image: RenderWhmModel(model, pose, opts), serial: serial}
	}()
}

// options builds the render request for the current camera and pose.
func (v *modelViewer) options(width, height, quality int) RenderOptions {
	var mats map[string]WhmMaterial
	if v.textured {
		mats = v.materials
	}
	return RenderOptions{
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
			BaseColor: rgbaWhite,
			Quality:   quality,
			Skeleton:  v.showSkeleton,
			// nil rather than v.markerIndex: the viewer's -1 is its own
			// "nothing picked", and the render request says the same thing
			// by carrying no marker at all.
			Marker:        pickedMarker(v.markerIndex),
			PickedBone:    pickedBone(v.boneIndex),
			UVDefault:     v.uv,
			UVPerMaterial: v.uvMaterials, MeshAlpha: v.meshAlphas(),
		},
		MaterialOverrides: mats,
		GroundImage:       v.groundImage,
		Ground:            v.showGround && v.groundImage != nil,
		FX:                v.fxScene,
	}
}

func (ui *uiState) renderModelOnGpu(gtx layout.Context, t *fileTab, w, h int) bool {
	v := t.model
	if v == nil {
		return false
	}
	reason := ui.modelGpuUnavailable(v)
	if reason != "" {
		// The software path takes over, so its frame has to be the one on
		// screen rather than whatever the hardware path drew last.
		if modelGpuSkipLogged != reason {
			log.Printf("3D preview: using software renderer (%s)", reason)
			modelGpuSkipLogged = reason
		}
		v.gpuFrame = nil
		return false
	}
	if modelGpuSkipLogged != "" {
		log.Printf("3D preview: hardware renderer taking over again (%s resolved)", modelGpuSkipLogged)
		modelGpuSkipLogged = ""
	}
	gpu := ui.modelGpuRenderer()
	opts := v.options(w, h, modelQualityFinal)
	var rsh func(string) ([]byte, bool)
	if v.textured && len(v.rshData) > 0 {
		rsh = func(material string) ([]byte, bool) {
			data, ok := v.rshData[material]
			return data, ok
		}
	}
	// The first frame of a stretch on the hardware path proves the render
	// itself completed — without it a silent window cannot be told apart
	// from a render that never returned.
	firstGPUFrame := v.gpuFrame == nil
	start := time.Now()
	frame, err := gpu.Render(v.model, v.pose(), opts, v.materials, rsh)
	if err == nil {
		d := time.Since(start)
		if firstGPUFrame {
			log.Printf("3D preview: GPU frame ready %dx%d in %v (serial=%d)", w, h, d, v.serial)
		}
		if d >= 50*time.Millisecond {
			log.Printf("3D preview: slow GPU render %v (%dx%d)", d, w, h)
		}
		// Read the frame back on the transitions that matter — the first
		// frame ever, the first one with textures bound, and the first one
		// after a resize — so a grey window can be told apart from a black
		// model and from lost draws.
		needProbe := v.gpuProbeW != w || v.gpuProbeH != h
		if rsh != nil && !v.gpuProbedTextured {
			needProbe = true
			v.gpuProbedTextured = true
		}
		if needProbe {
			v.gpuProbeW, v.gpuProbeH = w, h
			context := fmt.Sprintf("%dx%d textures=%v", w, h, rsh != nil)
			if !probeGpuFrame(frame, context) {
				// The device did not survive the readback. Every
				// later submit would fail the same way, so the
				// hardware path is given up on here rather than
				// one frame at a time.
				log.Printf("3D preview: hardware render gave up after the probe, falling back to software")
				v.gpuBroken = true
				v.gpuFrame = nil
				return false
			}
		}
	}
	if err != nil {
		// One failure is enough: the software path is always there.
		log.Printf("3D preview: hardware render failed, falling back to software: %v", err)
		v.gpuBroken = true
		v.gpuFrame = nil
		return false
	}
	v.gpuFrame = frame
	return true
}

// pollModelJobs runs on the UI goroutine and applies whatever the workers sent.
func (ui *uiState) pollModelJobs() {
	for _, t := range ui.openTabs {
		ui.pollViewerJobs(t)
	}
	// The object window's viewport has a viewer that is not a file tab, and it
	// renders and loads textures exactly the same way.
	ui.pollViewerJobs(ui.unitModel)
}

// pollViewerJobs takes whatever one viewer has finished: its textures, and the
// frame a worker rendered. A nil tab, or one with no model on it, is the normal
// case for most of them.
func (ui *uiState) pollViewerJobs(t *fileTab) {
	if t == nil || t.model == nil {
		return
	}
	v := t.model
	if t.modelMaterialCh != nil {
		select {
		case res := <-t.modelMaterialCh:
			t.modelMaterialCh = nil
			v.loading = false
			v.materialsLoaded = true
			v.materials = res.materials
			log.Printf("3D preview: textures ready: %d materials resolved, %d missing", len(res.materials), res.missing)
			if len(res.rsh) > 0 {
				v.rshData = res.rsh
			}
			v.textured = len(res.materials) > 0
			if res.missing > 0 {
				v.missing = res.missing
			}
			v.invalidate()
		default:
		}
	}
	if v.fxCh != nil {
		select {
		case res := <-v.fxCh:
			v.fxApply(res)
		default:
		}
	}
	if v.fxSoundCh != nil {
		select {
		case res := <-v.fxSoundCh:
			v.fxApplySoundsOnly(res)
		default:
		}
	}
	if v.frameCh != nil {
		select {
		case frame := <-v.frameCh:
			v.frameCh = nil
			v.rendering = false
			if frame.serial >= v.serial {
				v.serial = frame.serial
				v.frame = frame.image
				size := "nil"
				if frame.image != nil {
					b := frame.image.Bounds()
					size = fmt.Sprintf("%dx%d", b.Dx(), b.Dy())
				}
				log.Printf("3D preview: soft frame applied serial=%d size=%s", frame.serial, size)
			} else {
				// A frame the camera already moved past. Silent here it used
				// to be, which hides a render loop that gets nowhere.
				log.Printf("3D preview: soft frame DROPPED serial=%d current=%d", frame.serial, v.serial)
				v.redraw = true
			}
			// A camera move during the render means the frame is already
			// stale; start the next one right away.
			if v.pending {
				v.pending = false
				v.redraw = true
			}
		default:
		}
	}
}
