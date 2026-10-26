package main

import (
	"fmt"
	"image"
	"image/color"
	"log"
	"math"
	"path/filepath"
	"time"

	"dow-mod-studio/internal/3d_engine"
	"dow-mod-studio/internal/sgb"
	"dow-mod-studio/internal/whm"
	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"github.com/fogleman/fauxgl"
)

// .sgb viewer. Dawn of War ships its scenarios as .sgb chunky files; this is
// the first look inside one — the heightmap as a shaded top-down grid, with
// the terrain-type regions, the passability map and the placed decals as
// overlays. The terrain's real textures stay in the game's archives; until
// those decode, the overlays tint the map by the data the scenario itself
// carries and the header names the detail textures in use.

// Overlay bits of the sgb viewer's composite image. The image on the tab is
// always rebuilt from these, so the bits are the whole view state.
const (
	sgbOverTypes uint8 = 1 << iota
	sgbOverPass
	sgbOverDecals
	sgbOverEntities
)

func (ui *uiState) openSgb(virtualPath string) {
	if ui.modTree == nil {
		return
	}
	if idx := ui.findTabIndex(virtualPath); idx >= 0 {
		ui.tabSel = idx + 1
		ui.ensureTabClicks()
		ui.statusText = "Opened " + filepath.Base(virtualPath)
		return
	}
	t := &fileTab{path: virtualPath, kind: "sgb"}
	data, err := ui.modTree.ReadFile(virtualPath)
	if err != nil {
		t.sgbError = err.Error()
	} else {
		t.sgbBytes = len(data)
		parseStart := time.Now()
		m, perr := sgb.Parse(data)
		if perr != nil {
			t.sgbError = perr.Error()
		} else {
			t.sgbMap = m
			t.sgbImage = buildSgbImage(m, t.sgbMask)
			log.Printf("sgb: parsed %s in %v: %q relief %dx%d cells %dx%d @ %.2fm, %d decals, %d entities, %d textures",
				filepath.Base(virtualPath), time.Since(parseStart),
				m.Name, m.Relief.W, m.Relief.H, m.Ground.CellsX, m.Ground.CellsZ,
				m.Ground.CellSize, len(m.Decals), len(m.Entities), len(m.TexNames))
		}
	}
	ui.openTabs = append(ui.openTabs, t)
	ui.tabSel = len(ui.openTabs)
	ui.ensureTabClicks()
	if t.sgbError != "" {
		ui.statusText = "Open " + filepath.Base(virtualPath) + " failed: " + t.sgbError
	} else {
		ui.statusText = "Opened " + filepath.Base(virtualPath)
	}
}

func (ui *uiState) layoutSgbViewer(gtx layout.Context, t *fileTab) layout.Dimensions {
	if t == nil {
		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.th, unit.Sp(11), "No scenario selected")
			l.Color = cTextDim
			return l.Layout(gtx)
		})
	}
	if t.sgbError != "" {
		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.th, unit.Sp(11), "Failed: "+t.sgbError)
			l.Color = rgb(0xB00020)
			return l.Layout(gtx)
		})
	}
	m := t.sgbMap
	if m == nil || t.sgbImage == nil {
		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.th, unit.Sp(11), "No map data")
			l.Color = cTextDim
			return l.Layout(gtx)
		})
	}
	// The switch trades the flat map for the 3D terrain and back; both
	// views read the same parsed map, so nothing is reparsed on the way.
	if t.sgbViewSwitch.Clicked(gtx) {
		t.sgbView3D = !t.sgbView3D
	}
	if t.sgbView3D {
		ui.sgbEnsureTerrain(t)
	} else {
		// The overlays are part of the image, so a toggle rebuilds it once —
		// the maps are a million cells at most and the rebuild is milliseconds.
		for _, tg := range sgbToggles(t) {
			if tg.click.Clicked(gtx) {
				t.sgbMask ^= tg.bit
				t.sgbImage = buildSgbImage(m, t.sgbMask)
			}
		}
	}
	info := fmt.Sprintf("%s  ·  %s  ·  %d × %d cells  ·  %.0f × %.0f m  ·  %d decals  ·  %d entities  ·  %d textures",
		m.Name, m.Engine, m.Ground.CellsX, m.Ground.CellsZ,
		float64(m.Ground.CellsX)*float64(m.Ground.CellSize),
		float64(m.Ground.CellsZ)*float64(m.Ground.CellSize),
		len(m.Decals), len(m.Entities), len(m.TexNames))
	viewLabel := "3D"
	if t.sgbView3D {
		viewLabel = "2D"
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8), Top: unit.Dp(5), Bottom: unit.Dp(5)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						l := material.Label(ui.th, unit.Sp(10), info)
						l.Color = cTextDim
						l.MaxLines = 1
						return l.Layout(gtx)
					}),
					layout.Flexed(1, rowGap()),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						if t.sgbView3D {
							return layout.Dimensions{}
						}
						return ui.sgbOverlayBar(gtx, t)
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return ui.smallButton(gtx, &t.sgbViewSwitch, viewLabel)
					}),
				)
			})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, cHeaderLine, 1) }),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			if t.sgbView3D && t.model != nil {
				// The scroll panel measures its tabs with a synthetic
				// enormous height; rendering at that size recreates the
				// render target absurdly tall and fails the device. The
				// .whm viewer guards the same way (whmViewerLayout).
				if whmMeasurePass(gtx.Constraints.Max.Y) {
					return layout.Dimensions{Size: image.Pt(gtx.Constraints.Max.X, 0)}
				}
				gtx.Constraints.Min.Y = 0
				return ui.layoutModelViewer(gtx, t)
			}
			return ui.drawSgbCanvas(gtx, t)
		}),
	)
}

// sgbEnsureTerrain builds the tab's 3D terrain once, on the first switch to
// the 3D view: a heightmap mesh and the shaded relief as its diffuse map. The
// viewer is a plain model viewer, so the canvas, camera and both renderers
// need no sgb-specific path — only the material handed in here.
func (ui *uiState) sgbEnsureTerrain(t *fileTab) {
	if t == nil || t.sgbMap == nil || t.model != nil {
		return
	}
	start := time.Now()
	material := sgbTerrainMaterial(t.path)
	model := sgbTerrainModel(t.sgbMap, material)
	if model == nil {
		return
	}
	v := newModelViewer(model, t.path)
	// The framing a model viewer opens on is the whole model at 1.4× its
	// bounds — for a map that is hundreds of metres, the game's most
	// zoomed-out camera at its closest. A map opens on an in-game-like
	// distance instead, and its zoom range runs from a close orbit up to
	// the whole-map fit rather than a fraction of it.
	v.DefaultDistance = sgbCameraDefaultDistance(t.sgbMap)
	v.Distance = v.DefaultDistance
	if height, ok := sgbTerrainHeightAt(t.sgbMap, v.Center.X, v.Center.Y); ok {
		v.Center.Z = height
	}
	v.MinDistance = sgbCameraMinDistance
	v.MaxDistance = v.FitDistance
	v.Yaw, v.Pitch = sgbCameraYaw, sgbCameraPitch
	t.model = v
	// The diffuse — companion .tga, tile blend, every decal baked — and
	// the object scene on top of it cost seconds on a real map, so both
	// are built on a worker and arrive through the material channel every
	// viewer already polls: the ground first, the placed objects with
	// their materials after. Until then the terrain draws untextured
	// rather than the layout stalling.
	ui.startSgbSceneLoad(t, material)
	log.Printf("sgb: terrain %s ready in %v: %d verts, %d tris, material %q",
		filepath.Base(t.path), time.Since(start), len(model.Vertices), len(model.Meshes[0].Indices)/3, material)
}

// sgbToggle is one overlay switch of the viewer.
type sgbToggle struct {
	bit   uint8
	label string
	click *widget.Clickable
}

// sgbToggles is the viewer's switch strip in draw order.
func sgbToggles(t *fileTab) []sgbToggle {
	return []sgbToggle{
		{sgbOverTypes, "Terrain types", &t.sgbToggleTypes},
		{sgbOverPass, "Passability", &t.sgbTogglePass},
		{sgbOverDecals, "Decals", &t.sgbToggleDecals},
		{sgbOverEntities, "Entities", &t.sgbToggleEntities},
	}
}

// sgbOverlayBar draws the overlay switches: each button reads ✓ while its
// overlay is baked into the view.
func (ui *uiState) sgbOverlayBar(gtx layout.Context, t *fileTab) layout.Dimensions {
	toggles := sgbToggles(t)
	var children []layout.FlexChild
	for i := range toggles {
		if i > 0 {
			children = append(children, layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout))
		}
		tg := toggles[i]
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			mark := "· "
			if t.sgbMask&tg.bit != 0 {
				mark = "✓ "
			}
			return ui.smallButton(gtx, tg.click, mark+tg.label)
		}))
	}
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx, children...)
}

// drawSgbCanvas paints the composite map image scaled to fit the panel, the
// same affine paint the image viewer uses.
func (ui *uiState) drawSgbCanvas(gtx layout.Context, t *fileTab) layout.Dimensions {
	area := gtx.Constraints.Max
	if area.X < 1 || area.Y < 1 {
		return layout.Dimensions{}
	}
	gtx.Constraints.Min = area
	b := t.sgbImage.Bounds()
	scale := float32(area.X) / float32(b.Dx())
	if y := float32(area.Y) / float32(b.Dy()); y < scale {
		scale = y
	}
	if scale <= 0 {
		scale = 1
	}
	dw := int(float32(b.Dx())*scale + 0.5)
	dh := int(float32(b.Dy())*scale + 0.5)
	x := (area.X - dw) / 2
	y := (area.Y - dh) / 2
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			st := clip.Rect{Max: area}.Push(gtx.Ops)
			paint.ColorOp{Color: rgb(0x303030)}.Add(gtx.Ops)
			paint.PaintOp{}.Add(gtx.Ops)
			st.Pop()
			return layout.Dimensions{Size: area}
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			st := clip.Rect{Max: area}.Push(gtx.Ops)
			defer st.Pop()
			aff := op.Affine(f32.AffineId().Scale(f32.Pt(0, 0), f32.Pt(scale, scale)).Offset(f32.Pt(float32(x)-float32(b.Min.X)*scale, float32(y)-float32(b.Min.Y)*scale)))
			tr := aff.Push(gtx.Ops)
			paint.NewImageOp(t.sgbImage).Add(gtx.Ops)
			paint.PaintOp{}.Add(gtx.Ops)
			tr.Pop()
			return layout.Dimensions{Size: area}
		}),
	)
}

// buildSgbImage bakes the map into one NRGBA: the heightmap as a shaded grey
// relief, a faint grid every 64 cells, and the requested overlays blended on
// top — terrain-type regions, passability tint, decal dots.
func buildSgbImage(m *sgb.Map, mask uint8) *image.NRGBA {
	g := m.Relief
	if g.W < 2 || g.H < 2 || len(g.Data) < g.W*g.H {
		return nil
	}
	w, h := g.W, g.H
	// Heights in metres, straight from the byte grid and the world header's
	// scale. The relief shading is what shows the terrain, so metres keep the
	// slope maths independent of the stored byte range.
	height := func(x, y int) float32 {
		if x < 0 {
			x = 0
		}
		if y < 0 {
			y = 0
		}
		if x >= w {
			x = w - 1
		}
		if y >= h {
			y = h - 1
		}
		return float32(g.Data[y*w+x]) * m.Ground.HeightScale
	}
	lo, hi := float32(math.Inf(1)), float32(math.Inf(-1))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			v := height(x, y)
			if v < lo {
				lo = v
			}
			if v > hi {
				hi = v
			}
		}
	}
	span := hi - lo
	if span <= 0 {
		span = 1
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	// The relief reads a light from the north-west: the cell's slope dims
	// or brightens its grey, so ridges and craters show on a flat screen.
	// dz is the downhill in world units; reliefScale turns that into a grey
	// multiplier tuned so a one-cell cliff reads as a clear edge.
	reliefScale := float32(0.15) / span
	if reliefScale <= 0 {
		reliefScale = 0.15
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			v := height(x, y)
			t := (v - lo) / span
			dz := (height(x+1, y) - v) + (height(x, y+1) - v)
			shade := 1.0 - reliefScale*dz*10
			if shade < 0.4 {
				shade = 0.4
			}
			if shade > 1.5 {
				shade = 1.5
			}
			g := 35 + 165*t
			o := img.PixOffset(x, y)
			img.Pix[o+0] = sgbClampByte(g * shade)
			img.Pix[o+1] = sgbClampByte(g * shade)
			img.Pix[o+2] = sgbClampByte(g * shade)
			img.Pix[o+3] = 0xFF
		}
	}
	// The grid: a faint line every 64 cells keeps the image readable as a
	// grid of height samples rather than a picture.
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if x%64 != 0 && y%64 != 0 {
				continue
			}
			o := img.PixOffset(x, y)
			img.Pix[o+0] = sgbClampByte(float32(img.Pix[o+0]) * 0.75)
			img.Pix[o+1] = sgbClampByte(float32(img.Pix[o+1]) * 0.75)
			img.Pix[o+2] = sgbClampByte(float32(img.Pix[o+2]) * 0.75)
		}
	}
	if mask&sgbOverTypes != 0 {
		blendGrid(img, m.Terrain, func(v byte) (float32, float32, float32, float32) {
			switch v % 3 {
			case 0:
				return 0.75, 0.60, 0.35, 0.35
			case 1:
				return 0.45, 0.62, 0.35, 0.35
			default:
				return 0.60, 0.60, 0.62, 0.35
			}
		})
	}
	if mask&sgbOverPass != 0 {
		blendGrid(img, m.Pass, func(v byte) (float32, float32, float32, float32) {
			switch v {
			case 2: // passable
				return 0.30, 0.85, 0.35, 0.30
			case 1: // impassable
				return 0.90, 0.30, 0.25, 0.30
			default: // generated / unknown
				return 0.60, 0.60, 0.60, 0.12
			}
		})
	}
	if mask&sgbOverDecals != 0 {
		// Decals are placed in world metres; the world header converts a
		// metre to a cell index. A map that is 128×1024 keeps its own aspect
		// because each axis uses its own ground extent.
		for _, d := range m.Decals {
			px, py := sgbWorldToCell(m, d.X, d.Z)
			// A decal's footprint is its world size, rotated on the map; a
			// dot is enough here, so the rotation only steers which cells
			// the footprint tints.
			radius := sgbDecalRadius(m, d)
			for dy := -radius; dy <= radius; dy++ {
				for dx := -radius; dx <= radius; dx++ {
					x, y := px+dx, py+dy
					if x < 0 || y < 0 || x >= w || y >= h {
						continue
					}
					o := img.PixOffset(x, y)
					img.Pix[o+0] = 255
					img.Pix[o+1] = 64
					img.Pix[o+2] = 64
				}
			}
		}
	}
	if mask&sgbOverEntities != 0 {
		// Entities share the decals' world frame. Each marker is a bright
		// core with a short tick along the entity's heading, so rows of
		// walls read as aligned walls while single objects stay findable.
		for _, e := range m.Entities {
			px, py := sgbWorldToCell(m, e.X, e.Z)
			sgbEntityMark(img, px, py, math.Sin(float64(e.Heading)), math.Cos(float64(e.Heading)))
		}
	}
	return img
}

// sgbEntityMark draws one placed object: soft arm pixels around a bright
// core, then a three-pixel tick pointing along the entity's heading. Every
// pixel write is bounds checked, so an entity parked at the map edge still
// draws its in-bounds half.
func sgbEntityMark(img *image.NRGBA, x, y int, dx, dy float64) {
	b := img.Bounds()
	put := func(px, py int, r, g, bl float32) {
		if px < b.Min.X || py < b.Min.Y || px >= b.Max.X || py >= b.Max.Y {
			return
		}
		o := img.PixOffset(px, py)
		img.Pix[o+0] = sgbClampByte(r)
		img.Pix[o+1] = sgbClampByte(g)
		img.Pix[o+2] = sgbClampByte(bl)
	}
	// Arms first, core last: the core stays the brightest pixel.
	put(x-1, y, 170, 140, 40)
	put(x+1, y, 170, 140, 40)
	put(x, y-1, 170, 140, 40)
	put(x, y+1, 170, 140, 40)
	for s := 1; s <= 3; s++ {
		tx := x + int(math.Round(dx*float64(s)))
		ty := y + int(math.Round(dy*float64(s)))
		put(tx, ty, 255, 210, 60)
	}
	put(x, y, 255, 245, 160)
}

// sgbWorldToCell converts a world position in metres to a heightmap cell
// index, using the ground header's centre and cell size so a decal sits where
// the game would draw it.
func sgbWorldToCell(m *sgb.Map, worldX, worldZ float32) (int, int) {
	g := m.Ground
	if g.CellSize <= 0 {
		return 0, 0
	}
	cx := int(math.Round(float64(worldX)/float64(g.CellSize) + float64(g.CenterX-1)))
	cz := int(math.Round(float64(worldZ)/float64(g.CellSize) + float64(g.CenterZ-1)))
	return cx, cz
}

// sgbDecalRadius is the decal footprint in cells, from its world size. It is
// clamped so a stray size cannot tint the whole map.
func sgbDecalRadius(m *sgb.Map, d sgb.Decal) int {
	g := m.Ground
	if g.CellSize <= 0 || d.Size <= 0 {
		return 1
	}
	r := int(math.Round(float64(d.Size / g.CellSize / 2)))
	if r < 1 {
		return 1
	}
	if r > 64 {
		return 64
	}
	return r
}

// blendGrid tints the composite with a smaller grid, sampling it nearest and
// blending through the color's alpha.
func blendGrid(img *image.NRGBA, g sgb.Grid, colorFor func(v byte) (float32, float32, float32, float32)) {
	if g.W < 1 || g.H < 1 || len(g.Data) < g.W*g.H {
		return
	}
	b := img.Bounds()
	for y := 0; y < b.Dy(); y++ {
		gy := y * g.H / b.Dy()
		for x := 0; x < b.Dx(); x++ {
			gx := x * g.W / b.Dx()
			r, gr, bl, a := colorFor(g.Data[gy*g.W+gx])
			o := img.PixOffset(x, y)
			img.Pix[o+0] = sgbClampByte(float32(img.Pix[o+0])*(1-a) + 255*r*a)
			img.Pix[o+1] = sgbClampByte(float32(img.Pix[o+1])*(1-a) + 255*gr*a)
			img.Pix[o+2] = sgbClampByte(float32(img.Pix[o+2])*(1-a) + 255*bl*a)
		}
	}
}

// sgbClampByte is the viewer's float-to-byte clamp; it is separate from the
// fx overlay's float64 clampByte because all the relief maths here is float32.
func sgbClampByte(v float32) byte {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return byte(v + 0.5)
}

// ─── merged from sgb_selection.go ───────────────────────────────────────────────

// paintSgbSelection draws the software fallback's selection bounds and keeps
// the drag marquee in the UI. Vulkan draws selection bounds into its color target.
func paintSgbSelection(gtx layout.Context, tab *fileTab, viewer *modelViewer, width, height int, drawBounds bool) {
	area := image.Rectangle{Max: image.Pt(width, height)}
	canvasClip := clip.Rect{Max: area.Max}.Push(gtx.Ops)
	defer canvasClip.Pop()
	if tab == nil || viewer == nil || tab.sgbMap == nil || width <= 0 || height <= 0 {
		return
	}
	if drawBounds && viewer.model != nil && len(tab.sgbSelectedEntities) > 0 {
		pose := viewer.pose()
		if len(viewer.animatedEntities) > 0 {
			pose = sgb.CombineEntityPoses(len(pose.Bones), viewer.animatedEntities)
		}
		bounds := sgbEntityProjectedBounds(viewer.modelData(), tab.sgbEntityMeshIDs, buildViewMatrix(viewer.options(width, height)), width, height, pose, len(pose.Bones) > 0)
		for id := range tab.sgbSelectedEntities {
			box := bounds[id].Inset(-3).Intersect(area)
			if !box.Empty() {
				paint.FillShape(gtx.Ops, color.NRGBA{R: 255, G: 220, B: 80, A: 220}, clip.Stroke{Path: sgbRectPath(gtx, box), Width: 2}.Op())
			}
		}
	}
	paintSgbSelectionMarquee(gtx, tab, area)
}

// prepareSgbSelectionBounds converts projected bounds for selected entities
// into four camera-facing world-space strips for the Vulkan overlay pipeline.
func prepareSgbSelectionBounds(model *whm.Model, selected map[int]bool, entityMeshes map[string]int, view fauxgl.Matrix, camera engine.Camera, pose whm.Pose, animated bool) []modelOverlayLine {
	if model == nil || len(selected) == 0 || camera.Width <= 0 || camera.Height <= 0 || math.Abs(view.Determinant()) < 1e-12 {
		return nil
	}
	bounds := sgbEntityProjectedBounds(model, entityMeshes, view, camera.Width, camera.Height, pose, animated)
	inverse := view.Inverse()
	halfWidth := engine.OverlayHalfWidth(engine.OverlayOptions{Camera: camera}, 1.1)
	var lines []modelOverlayLine
	for id := range selected {
		box := bounds[id]
		if box.Empty() {
			continue
		}
		// Mirror the software outline's small margin, then reconstruct the
		// four corners at the entity's nearest projected depth. The overlay
		// pipeline draws these strips into the Vulkan color target without a
		// depth test, keeping the selection visible over the selected mesh.
		box = box.Inset(-3)
		depth := sgbEntityNearestDepth(model, entityMeshes, id, view, pose, animated)
		corners := [...]image.Point{box.Min, {X: box.Max.X, Y: box.Min.Y}, box.Max, {X: box.Min.X, Y: box.Max.Y}}
		var points [4]fauxgl.Vector
		valid := true
		for i, point := range corners {
			clipPoint := fauxgl.Vector{
				X: 2*float64(point.X)/float64(camera.Width) - 1,
				Y: 1 - 2*float64(point.Y)/float64(camera.Height),
				Z: math.Max(0, depth-0.001),
			}
			world := inverse.MulPositionW(clipPoint)
			if math.Abs(world.W) < 1e-12 {
				valid = false
				break
			}
			points[i] = fauxgl.Vector{X: world.X / world.W, Y: world.Y / world.W, Z: world.Z / world.W}
		}
		if !valid {
			continue
		}
		for edge := range 4 {
			lines = append(lines, modelOverlayLine{From: points[edge], To: points[(edge+1)%4], HalfWidth: halfWidth, R: 1, G: 0.86, B: 0.31, A: 0.92})
		}
	}
	return lines
}

func sgbEntityNearestDepth(model *whm.Model, entityMeshes map[string]int, entityID int, view fauxgl.Matrix, pose whm.Pose, animated bool) float64 {
	nearest := math.Inf(1)
	for _, mesh := range model.Meshes {
		id, ok := entityMeshes[mesh.Name]
		if !ok || id != entityID || mesh.Name == sgb.WaterMeshName {
			continue
		}
		for _, index := range mesh.Indices {
			if int(index) >= len(model.Vertices) {
				continue
			}
			vertex := &model.Vertices[index]
			position := vectorFromWhm(vertex.Pos)
			if animated {
				position, _ = whm.SkinnedVertex(model, pose, vertex)
			}
			projected := view.MulPositionW(position)
			if projected.W <= 1e-6 {
				continue
			}
			depth := projected.Z / projected.W
			if depth >= 0 && depth < nearest {
				nearest = depth
			}
		}
	}
	if math.IsInf(nearest, 1) {
		return 1
	}
	return nearest
}

func paintSgbSelectionMarquee(gtx layout.Context, tab *fileTab, area image.Rectangle) {
	if tab == nil || !tab.sgbSelecting || !tab.sgbSelectionDrag {
		return
	}
	box := sgbSelectionRect(tab.sgbSelectionAnchor, tab.sgbSelectionCurrent).Intersect(area)
	if box.Empty() {
		return
	}
	paint.FillShape(gtx.Ops, color.NRGBA{R: 90, G: 170, B: 255, A: 30}, clip.Rect{Min: box.Min, Max: box.Max}.Op())
	paint.FillShape(gtx.Ops, color.NRGBA{R: 110, G: 190, B: 255, A: 230}, clip.Stroke{Path: sgbRectPath(gtx, box), Width: 1}.Op())
}

// sgbEntityProjectedBounds projects all referenced vertices and combines every
// submesh into one screen-space rectangle per entity. Animated meshes use the
// same skinning pose as the renderer.
func sgbEntityProjectedBounds(model *whm.Model, entityMeshes map[string]int, view fauxgl.Matrix, width, height int, pose whm.Pose, animated bool) map[int]image.Rectangle {
	bounds := make(map[int]image.Rectangle)
	if model == nil || width <= 0 || height <= 0 {
		return bounds
	}
	for _, mesh := range model.Meshes {
		if mesh.Name == sgb.WaterMeshName {
			continue
		}
		id, ok := entityMeshes[mesh.Name]
		if !ok {
			continue
		}
		for _, index := range mesh.Indices {
			if int(index) >= len(model.Vertices) {
				continue
			}
			vertex := &model.Vertices[index]
			p := vectorFromWhm(vertex.Pos)
			if animated {
				p, _ = whm.SkinnedVertex(model, pose, vertex)
			}
			projected := view.MulPositionW(p)
			if projected.W <= 1e-6 {
				continue
			}
			x := (projected.X/projected.W*0.5 + 0.5) * float64(width)
			y := (1 - (projected.Y/projected.W*0.5 + 0.5)) * float64(height)
			point := image.Rect(int(math.Floor(x)), int(math.Floor(y)), int(math.Floor(x))+1, int(math.Floor(y))+1)
			if current := bounds[id]; current.Empty() {
				bounds[id] = point
			} else {
				bounds[id] = current.Union(point)
			}
		}
	}
	return bounds
}

// sgbCanvasToFramePoint undoes the frame's scale and letterboxing so pointer
// coordinates and projected scene pixels share one coordinate system.
func sgbCanvasToFramePoint(area image.Point, frameWidth, frameHeight int, point f32.Point) image.Point {
	if area.X <= 0 || area.Y <= 0 || frameWidth <= 0 || frameHeight <= 0 {
		return image.Point{}
	}
	scale := math.Min(float64(area.X)/float64(frameWidth), float64(area.Y)/float64(frameHeight))
	offsetX := (float64(area.X) - float64(frameWidth)*scale) / 2
	offsetY := (float64(area.Y) - float64(frameHeight)*scale) / 2
	x := int(math.Round((float64(point.X) - offsetX) / scale))
	y := int(math.Round((float64(point.Y) - offsetY) / scale))
	return image.Pt(maxInt(x, 0), maxInt(y, 0))
}

func sgbPickEntity(model *whm.Model, entityMeshes map[string]int, camera engine.Camera, pixelX, pixelY float64) (int, bool) {
	if model == nil {
		return 0, false
	}
	view := engine.ViewMatrix(camera)
	if math.Abs(view.Determinant()) < 1e-12 {
		return 0, false
	}
	inverse := view.Inverse()
	nx, ny := 2*pixelX/float64(camera.Width)-1, 1-2*pixelY/float64(camera.Height)
	near := inverse.MulPositionW(fauxgl.Vector{X: nx, Y: ny, Z: -1})
	far := inverse.MulPositionW(fauxgl.Vector{X: nx, Y: ny, Z: 1})
	if math.Abs(near.W) < 1e-12 || math.Abs(far.W) < 1e-12 {
		return 0, false
	}
	origin := fauxgl.Vector{X: near.X / near.W, Y: near.Y / near.W, Z: near.Z / near.W}
	direction := fauxgl.Vector{X: far.X/far.W - origin.X, Y: far.Y/far.W - origin.Y, Z: far.Z/far.W - origin.Z}
	closest := math.Inf(1)
	picked := 0
	for _, mesh := range model.Meshes {
		id, ok := entityMeshes[mesh.Name]
		if !ok || mesh.Name == sgb.WaterMeshName {
			continue
		}
		for i := 0; i+2 < len(mesh.Indices); i += 3 {
			indices := mesh.Indices[i : i+3]
			if int(indices[0]) >= len(model.Vertices) || int(indices[1]) >= len(model.Vertices) || int(indices[2]) >= len(model.Vertices) {
				continue
			}
			vertices := [3]fauxgl.Vector{}
			for j, index := range indices {
				vertices[j] = vectorFromWhm(model.Vertices[index].Pos)
			}
			if distance, hit := sgbRayTriangle(origin, direction, vertices[0], vertices[1], vertices[2]); hit && distance < closest {
				closest, picked = distance, id
			}
		}
	}
	return picked, !math.IsInf(closest, 1)
}

func sgbRectangleEntities(model *whm.Model, entityMeshes map[string]int, view fauxgl.Matrix, width, height int, rect image.Rectangle) map[int]bool {
	selected := make(map[int]bool)
	if model == nil || width <= 0 || height <= 0 {
		return selected
	}
	rect = rect.Canon()
	for _, mesh := range model.Meshes {
		id, ok := entityMeshes[mesh.Name]
		if !ok || mesh.Name == sgb.WaterMeshName {
			continue
		}
		for i := 0; i+2 < len(mesh.Indices); i += 3 {
			idx := mesh.Indices[i : i+3]
			if int(idx[0]) >= len(model.Vertices) || int(idx[1]) >= len(model.Vertices) || int(idx[2]) >= len(model.Vertices) {
				continue
			}
			var points [3]image.Point
			valid := true
			for j, index := range idx {
				projected := view.MulPositionW(vectorFromWhm(model.Vertices[index].Pos))
				if projected.W <= 1e-6 {
					valid = false
					break
				}
				x := (projected.X/projected.W*0.5 + 0.5) * float64(width)
				y := (1 - (projected.Y/projected.W*0.5 + 0.5)) * float64(height)
				points[j] = image.Pt(int(math.Round(x)), int(math.Round(y)))
			}
			if valid && sgbTriangleIntersectsRect(points, rect) {
				selected[id] = true
				break
			}
		}
	}
	return selected
}

func sgbTriangleIntersectsRect(triangle [3]image.Point, rect image.Rectangle) bool {
	if rect.Empty() {
		return false
	}
	for _, point := range triangle {
		if point.In(rect) {
			return true
		}
	}
	corners := [...]image.Point{rect.Min, {X: rect.Max.X - 1, Y: rect.Min.Y}, {X: rect.Min.X, Y: rect.Max.Y - 1}, {X: rect.Max.X - 1, Y: rect.Max.Y - 1}}
	for _, corner := range corners {
		if sgbPointInTriangle(corner, triangle) {
			return true
		}
	}
	for edge := 0; edge < 3; edge++ {
		a, b := triangle[edge], triangle[(edge+1)%3]
		for side := 0; side < 4; side++ {
			c, d := corners[side], corners[(side+1)%4]
			if sgbSegmentsIntersect(a, b, c, d) {
				return true
			}
		}
	}
	return false
}

func sgbPointInTriangle(point image.Point, triangle [3]image.Point) bool {
	orientation := func(a, b, p image.Point) int64 {
		return int64(b.X-a.X)*int64(p.Y-a.Y) - int64(b.Y-a.Y)*int64(p.X-a.X)
	}
	d0 := orientation(triangle[0], triangle[1], point)
	d1 := orientation(triangle[1], triangle[2], point)
	d2 := orientation(triangle[2], triangle[0], point)
	return !(d0 < 0 || d1 < 0 || d2 < 0) || !(d0 > 0 || d1 > 0 || d2 > 0)
}

func sgbSegmentsIntersect(a, b, c, d image.Point) bool {
	orientation := func(p, q, r image.Point) int64 {
		return int64(q.X-p.X)*int64(r.Y-p.Y) - int64(q.Y-p.Y)*int64(r.X-p.X)
	}
	onSegment := func(p, q, r image.Point) bool {
		return q.X >= minInt(p.X, r.X) && q.X <= maxInt(p.X, r.X) && q.Y >= minInt(p.Y, r.Y) && q.Y <= maxInt(p.Y, r.Y)
	}
	o1, o2, o3, o4 := orientation(a, b, c), orientation(a, b, d), orientation(c, d, a), orientation(c, d, b)
	if (o1 < 0 && o2 > 0 || o1 > 0 && o2 < 0) && (o3 < 0 && o4 > 0 || o3 > 0 && o4 < 0) {
		return true
	}
	return o1 == 0 && onSegment(a, c, b) || o2 == 0 && onSegment(a, d, b) || o3 == 0 && onSegment(c, a, d) || o4 == 0 && onSegment(c, b, d)
}

func sgbSelectionRect(a, b image.Point) image.Rectangle {
	return image.Rect(minInt(a.X, b.X), minInt(a.Y, b.Y), maxInt(a.X, b.X), maxInt(a.Y, b.Y))
}

func sgbRectPath(gtx layout.Context, rect image.Rectangle) clip.PathSpec {
	var path clip.Path
	path.Begin(gtx.Ops)
	path.MoveTo(f32.Pt(float32(rect.Min.X), float32(rect.Min.Y)))
	path.LineTo(f32.Pt(float32(rect.Max.X), float32(rect.Min.Y)))
	path.LineTo(f32.Pt(float32(rect.Max.X), float32(rect.Max.Y)))
	path.LineTo(f32.Pt(float32(rect.Min.X), float32(rect.Max.Y)))
	path.Close()
	return path.End()
}

// ─── merged from sgb_camera_target.go ───────────────────────────────────────────────

// sgbCameraTargetScreen projects the orbit target into the renderer's pixel
// space, shared by the software and GPU overlay paths.
func sgbCameraTargetScreen(v *modelViewer, width, height int) (fauxgl.Vector, bool) {
	if v == nil || width < 1 || height < 1 {
		return fauxgl.Vector{}, false
	}
	at := fxProject(buildViewMatrix(v.options(width, height)), v.Center, width, height)
	if at == nil {
		return fauxgl.Vector{}, false
	}
	return *at, true
}

// paintSgbCameraTargetOverlay draws nested contrasting circles so the current
// surface point remains easy to spot against both terrain and placed objects.
func (v *modelViewer) paintSgbCameraTargetOverlay(gtx layout.Context, width, height int) {
	at, ok := sgbCameraTargetScreen(v, width, height)
	if !ok {
		return
	}
	x, y := int(at.X+0.5), int(at.Y+0.5)
	for _, mark := range []struct {
		radius int
		color  color.NRGBA
	}{
		{15, color.NRGBA{R: 12, G: 14, B: 18, A: 245}},
		{11, color.NRGBA{R: 255, G: 194, B: 40, A: 255}},
		{7, color.NRGBA{R: 16, G: 20, B: 25, A: 255}},
		{3, color.NRGBA{R: 255, G: 255, B: 240, A: 255}},
	} {
		bounds := image.Rect(x-mark.radius, y-mark.radius, x+mark.radius+1, y+mark.radius+1)
		paint.FillShape(gtx.Ops, mark.color, clip.Ellipse(bounds).Op(gtx.Ops))
	}
}
