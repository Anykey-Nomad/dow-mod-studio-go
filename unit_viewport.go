package main

import (
	viewer "dow-mod-studio/internal/3d_viewer"
	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/rgd"
	"dow-mod-studio/internal/sgb"
	"dow-mod-studio/internal/whe"
	"errors"
	"fmt"
	"image"
	"math"
	"math/rand"
	"path/filepath"
	"strings"

	"github.com/fogleman/fauxgl"
)

// The object window's Viewport section: the selected unit's own 3D model, with
// its animations.
//
// Dawn of War keeps a unit's geometry out of the .rgd. The .rgd names it in
// entity_blueprint_ext.animator, a path relative to Data\art\ebps that ends in
// the model's own name, e.g. "Races/Necrons/Troops/Necron_Basic_Warrior", and
// the model is the .whm of that name in that folder. Measured over the 582
// unit .rgds of the DXP3 tree that resolves for 581 of them, so it is the
// lookup; the rest fall back to a search of the art tree for a .whm of the
// unit's own name, which is how a mod that renames the model still gets one.
//
// The viewer itself is not new. It is the same one a .whm tab uses, driven
// from a fileTab held here rather than in openTabs, so a unit's model can be
// orbited, played and inspected without opening it first.

// unitSectionViewport names the section, so the layout does not repeat the
// string of the menu entry it is keyed on.
const unitSectionViewport = "Viewport"

// unitModelArtRoot is the folder animator paths are relative to.
const unitModelArtRoot = "art/ebps"

// unitGroundTexture is the default terrain texture shown beneath unit models.
const unitGroundTexture = "Data/art/scenarios/textures/detail/jungle_ground_02.tga"

// errNoUnitModel is what a unit whose model cannot be found anywhere reports,
// as distinct from a model that was found and would not decode.
var errNoUnitModel = errors.New("no model file for this unit")

// unitModelResult carries a decoded model back to the UI goroutine. The unit it
// was read for travels with it, since a load started for one unit can finish
// after the reader has moved on to another.
type unitModelResult struct {
	unit        string
	path        string
	model       *WhmModel
	groundImage image.Image
	err         error
}

// unitModelSource is everything about a unit that the loader needs, copied off
// the UI goroutine before the load starts so the worker touches no UI state.
type unitModelSource struct {
	unit     string // the unit .rgd's tree path
	animator string // entity_blueprint_ext.animator, may be empty
	name     string // the unit's file name without extension
	race     string // the folder name under races
	kind     string // "troops" or "structures"
}

// newUnitModelSource reads what a unit says about its model.
func newUnitModelSource(tree *modtree.ModTree, d *unitDetails) (unitModelSource, bool) {
	if d == nil {
		return unitModelSource{}, false
	}
	src := unitModelSource{
		unit: d.entry.path,
		name: d.entry.name,
		race: d.entry.race,
		kind: d.entry.kind.folder,
	}
	if src.name == "" {
		if base := d.entry.path; base != "" {
			src.name = strings.TrimSuffix(filepath.Base(base), filepath.Ext(base))
		}
	}
	if d.file != nil {
		src.animator = unitAnimatorName(d.file)
	}
	if src.name == "" && src.animator == "" {
		return unitModelSource{}, false
	}
	return src, true
}

// unitAnimatorName reads entity_blueprint_ext.animator out of a unit .rgd, or
// "" when the file does not carry one. The field is a plain string, not a
// reference: it names a model folder path rather than an asset the .rgd
// resolves, which is why it is read here rather than through resolveRgdRef.
func unitAnimatorName(rf *rgd.RgdFile) string {
	if rf == nil || rf.Root == nil {
		return ""
	}
	ext := rgdFindChild(rf.Root, "entity_blueprint_ext")
	if ext == nil {
		return ""
	}
	e := rgdFindChild(ext, "animator")
	if e == nil {
		return ""
	}
	return strings.TrimSpace(e.DisplayValue())
}

// unitModelPath finds the .whm of a unit, or "" when there is none. The order
// is the one that works on the shipped data first and the one that rescues a
// mod's own naming last.
func unitModelPath(tree *modtree.ModTree, src unitModelSource) string {
	if tree == nil {
		return ""
	}
	rel := strings.ReplaceAll(strings.TrimSpace(src.animator), "\\", "/")
	rel = strings.TrimPrefix(rel, "./")
	if rel != "" {
		if !strings.Contains(rel, ".") {
			rel += ".whm"
		}
		p := filepath.Join("Data", filepath.FromSlash(unitModelArtRoot), filepath.FromSlash(rel))
		if tree.FindFile(p) != nil {
			return p
		}
	}
	// The folder the unit's .rgd itself lives in is where the game keeps the
	// model of a unit it renamed, so the sibling name is worth a try before
	// searching the whole tree.
	if src.race != "" && src.name != "" {
		dirs := []string{}
		if src.kind != "" {
			dirs = append(dirs, src.kind)
		}
		dirs = append(dirs, "", "troops", "structures")
		for _, d := range dirs {
			parts := []string{"Data", unitModelArtRoot, "races", src.race}
			if d != "" {
				parts = append(parts, d)
			}
			parts = append(parts, src.name+".whm")
			p := filepath.Join(parts...)
			if tree.FindFile(p) != nil {
				return p
			}
		}
	}
	if src.name != "" {
		if f := tree.FindByName(src.name + ".whm"); f != nil {
			return f.FullPath
		}
	}
	return ""
}

// ensureUnitModel starts loading the current unit's model if it is not already
// loaded or on its way. It is called from the Viewport section's layout, so
// browsing units never pays for a model that is not being looked at.
func (ui *uiState) ensureUnitModel() {
	if ui.modTree == nil {
		return
	}
	src, ok := newUnitModelSource(ui.modTree, ui.currentUnitDetails())
	if !ok {
		ui.resetUnitModel()
		return
	}
	if src.unit == ui.unitModelFor && (ui.unitModel != nil || ui.unitModelCh != nil || ui.unitModelErr != "") {
		return
	}
	ui.startUnitModel(src)
}

// startUnitModel kicks the read and the parse off on a worker. Both are worth
// moving off the UI goroutine: a unit model is megabytes, and the necron warrior's
// alone is five of them.
func (ui *uiState) startUnitModel(src unitModelSource) {
	ui.resetUnitModel()
	ui.unitModelFor = src.unit
	tree := ui.modTree
	ch := make(chan unitModelResult, 1)
	ui.unitModelCh = ch
	go func() {
		res := unitModelResult{unit: src.unit}
		res.path = unitModelPath(tree, src)
		if res.path == "" {
			res.err = errNoUnitModel
			ch <- res
			return
		}
		data, err := tree.ReadFile(res.path)
		if err != nil {
			res.err = err
			ch <- res
			return
		}
		model, err := ParseWhmModel(data)
		if err != nil {
			res.err = err
			ch <- res
			return
		}
		res.model = model
		res.groundImage = loadWhmGroundImage(tree)
		ch <- res
	}()
}

// resetUnitModel drops the viewer and forgets which unit it was for, so the
// next request loads again. The worker is not stopped: it writes into a
// buffered channel nobody reads, which costs one model parse and no more.
func (ui *uiState) resetUnitModel() {
	if ui.unitModel != nil && ui.unitModel.model != nil {
		// The hardware renderer caches the model's buffers, and the goroutine
		// that may be mid render holds the same pointer, so both go back.
		if ui.modelGpu != nil && ui.unitModel.model.model != nil {
			ui.modelGpu.forget(ui.unitModel.model.model)
		}
		ui.unitModel.model.close()
	}
	ui.unitModel = nil
	ui.unitModelCh = nil
	ui.unitModelFor = ""
	ui.unitModelPath = ""
	ui.unitModelErr = ""
	ui.unitAnimClicks = nil
	ui.unitCamClicks = nil

	ui.unitMarkerClicks = nil
	ui.unitBoneClicks = nil
	ui.unitFXListMode = false
	ui.unitMeshListMode = false
	ui.unitViewportScroll = panelScrollState{}
	// The typed bone key fields belong to the model that has gone with it:
	// their bound key and any pending note are dropped so a later model
	// cannot be shown with, or written from, this one's values.
	ui.unitModelWhmDirty = false
	ui.unitBoneKeyBound = ""
	ui.unitBoneKeyErr = ""
}

// pollUnitModel takes a finished decode and builds the viewer from it. The
// frames and the textures it goes on to produce are polled by pollModelJobs,
// like any other viewer's.
// selectObjectAction reads the model's WHE graph and plays all clips
// selected by the named action together. Weighted motion links choose one
// variant independently for each motion; absent or unresolved WHE data leaves
// the viewport's ordinary single-clip default in place. The idle action also
// falls back to the pristine state's idle/stand clips when the graph has no
// idle action of its own.
func selectObjectAction(v *modelViewer, tree *modtree.ModTree, modelPath, actionName string) bool {
	if v == nil || v.model == nil || tree == nil || modelPath == "" || strings.TrimSpace(actionName) == "" {
		return false
	}
	base := strings.TrimSuffix(modelPath, filepath.Ext(modelPath))
	data, err := tree.ReadFile(base + ".whe")
	if err != nil {
		if f := tree.FindByName(strings.ToLower(filepath.Base(base) + ".whe")); f != nil {
			data, err = tree.ReadTreeFile(f)
		}
	}
	if err != nil || len(data) == 0 {
		return false
	}
	doc, err := whe.ParseDocument(data)
	if err != nil {
		return false
	}
	seed := rand.Int63()
	clipNames := doc.ActionAnimationNamesWithSeed(actionName, seed)
	if len(clipNames) == 0 && strings.EqualFold(strings.TrimSpace(actionName), "idle") {
		for _, name := range doc.RestClipNamesWithSeed(seed) {
			lower := strings.ToLower(strings.TrimSpace(name))
			if strings.HasPrefix(lower, "idle") || strings.HasPrefix(lower, "stand") {
				clipNames = append(clipNames, name)
			}
		}
	}
	var clips []*WhmAnimation
	seen := make(map[string]bool)
	for _, name := range clipNames {
		key := strings.ToLower(strings.TrimSpace(name))
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		animation, source := sgb.ResolveLinkedAnimation(tree, modelPath, &v.model.Model, name)
		if animation == nil || source == nil {
			continue
		}
		if source != &v.model.Model {
			animation = sgb.BindAnimationToModel(&v.model.Model, animation)
		}
		if animation != nil {
			clips = append(clips, animation)
		}
	}
	if len(clips) == 0 {
		return false
	}
	v.selectAnimations(clips, true)
	v.playing = true
	return true
}

// activateUnitAction plays every clip the named action of the loaded unit
// model's .whe resolves to, combined in the viewport, and says so in the
// status bar. It answers false when no model is on screen or the action
// resolves to nothing, leaving the caller with its own fallback.
func (ui *uiState) activateUnitAction(actionName string) bool {
	v := ui.unitModelViewer()
	if v == nil || v.model == nil {
		return false
	}
	if !selectObjectAction(v, ui.modTree, ui.unitModelPath, actionName) {
		return false
	}
	ui.statusText = fmt.Sprintf("Action %q — playing %d clip(s) together",
		strings.TrimSpace(actionName), len(v.anims))
	return true
}

func (ui *uiState) pollUnitModel() {
	if ui.unitModelCh == nil {
		return
	}
	select {
	case res := <-ui.unitModelCh:
		ui.unitModelCh = nil
		if res.unit != ui.unitModelFor {
			// The reader has moved on to another unit while this one loaded.
			return
		}
		if res.err != nil {
			ui.unitModelErr = res.err.Error()
			return
		}
		tab := &fileTab{path: res.path, kind: "whm"}
		tab.model = newObjectModelViewer(res.model, res.path, res.groundImage)
		selectObjectAction(tab.model, ui.modTree, res.path, "idle")
		// The canvas is a third of the window wide here, so the playback strip
		// is drawn in the panel beside it rather than under it.
		tab.model.sideControls = true
		ui.unitModel = tab
		ui.unitModelPath = res.path
		// A freshly read model starts clean, and its keys rebind the typed
		// fields: the previous model's edits are not its edits.
		ui.unitModelWhmDirty = false
		ui.unitBoneKeyBound = ""
		ui.unitBoneKeyErr = ""
	default:
	}
}

// unitDefaultAnim picks the clip the viewport starts on, or -1 when the model
// has none. The names are the game's own: a unit moves with "move" and "run",
// stands with "idle_1", and everything else in the list is a one off.
func unitDefaultAnim(model *WhmModel) int {
	if model == nil || len(model.Animations) == 0 {
		return -1
	}
	want := []string{"move", "run", "walk", "idle_1", "idle_2", "idle", "idle_firing"}
	names := make([]string, len(model.Animations))
	for i := range model.Animations {
		names[i] = strings.ToLower(model.Animations[i].Name)
	}
	for _, w := range want {
		for i, n := range names {
			if n == w {
				return i
			}
		}
	}
	// A clip whose name starts "vis_" only fades a weapon or a banner in and
	// out; it moves nothing, so it is a poor thing to open on.
	for i, n := range names {
		if !strings.HasPrefix(n, "vis_") {
			return i
		}
	}
	return 0
}

// unitModelViewer is the viewport's viewer, or nil when no model is up.
func (ui *uiState) unitModelViewer() *modelViewer {
	if ui.unitModel == nil {
		return nil
	}
	return ui.unitModel.model
}

// lookThroughCamera moves the orbit camera to where one of the model's own
// cameras is, which is what makes the list of them worth having. A .whm camera
// is a local transform in the exporter's frame, so it is put into model space
// by the same rotation the skeleton's root bones get, and it looks down its own
// -Z at its focus point. The direction is turned into the orbit camera's pair
// the way buildViewMatrix takes it apart: the yaw around the up axis, which is
// Z, and the height off the horizon.
func (v *modelViewer) lookThroughCamera(cam WhmCamera) {
	if v == nil || v.model == nil {
		return
	}
	eye := boneRootFrame.MulPosition(vec3From(cam.Pos))
	focus := boneRootFrame.MulPosition(vec3From(cam.FocusPoint))
	v.CameraState = viewer.LookThroughCamera(v.CameraState, eye, focus, cam.Fov, cam.Name)
	v.invalidate()
}

// resetCamera puts the orbit camera back to the framing newModelViewer chose,
// and forgets any camera of the model's that was being looked through.
func (v *modelViewer) resetCamera() {
	if v == nil {
		return
	}
	camera := viewer.ResetCamera(v.CameraState)
	v.CameraState = camera
	v.invalidate()
}

// panCamera shifts the orbit target in the camera's screen plane. Pixel motion
// is scaled by the world-space width visible at the camera distance, so the
// drag has consistent sensitivity across zoom levels and viewport sizes.
func (v *modelViewer) panCamera(dx, dy float64, height int) {
	if v == nil || height <= 0 || v.Distance <= 0 {
		return
	}
	forward := fauxgl.Vector{
		X: -math.Cos(v.Yaw) * math.Cos(v.Pitch),
		Y: -math.Sin(v.Yaw) * math.Cos(v.Pitch),
		Z: -math.Sin(v.Pitch),
	}
	worldUp := fauxgl.Vector{Z: 1}
	right := forward.Cross(worldUp)
	if length := right.Length(); length > 1e-9 {
		right = right.MulScalar(1 / length)
	} else {
		right = fauxgl.Vector{X: 1}
	}
	up := right.Cross(forward)
	if length := up.Length(); length > 1e-9 {
		up = up.MulScalar(1 / length)
	}
	worldPerPixel := 2 * v.Distance * math.Tan(v.FOV*math.Pi/360) / float64(height)
	v.Center = v.Center.Sub(right.MulScalar(dx * worldPerPixel)).Add(up.MulScalar(dy * worldPerPixel))
	v.ActiveCamera = ""
	v.invalidate()
}
