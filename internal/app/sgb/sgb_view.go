package sgb

import (
	engine "dow-mod-studio/internal/3d_engine"
	"dow-mod-studio/internal/app/render"
	"dow-mod-studio/internal/app/uikit"
	"dow-mod-studio/internal/app/uistate"
	"dow-mod-studio/internal/app/viewer"
	"dow-mod-studio/internal/sgb"
	"dow-mod-studio/internal/whm"
	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"github.com/fogleman/fauxgl"
	"image"
	"image/color"
	"log"
	"math"
	"path/filepath"
	"time"
)

// Overlay bits of the sgb viewer's composite image. The image on the tab is
// always rebuilt from these, so the bits are the whole view state.
const (
	SgbOverTypes uint8 = 1 << iota
	SgbOverPass
	SgbOverDecals
	SgbOverEntities
)

func OpenSgb(ui *uistate.State, virtualPath string) {
	if ui.ModTree == nil {
		return
	}
	if idx := uikit.FindTabIndex(ui, virtualPath); idx >= 0 {
		ui.TabSel = idx + 1
		uikit.EnsureTabClicks(ui)
		ui.StatusText = "Opened " + filepath.Base(virtualPath)
		return
	}
	t := &uistate.Tab{Path: virtualPath, Kind: "sgb"}
	data, err := ui.ModTree.ReadFile(virtualPath)
	if err != nil {
		t.SgbError = err.Error()
	} else {
		t.SgbBytes = len(data)
		parseStart := time.Now()
		m, perr := sgb.Parse(data)
		if perr != nil {
			t.SgbError = perr.Error()
		} else {
			t.SgbMap = m
			t.SgbImage = BuildSgbImage(m, t.SgbMask)
			log.Printf("sgb: parsed %s in %v: %q relief %dx%d cells %dx%d @ %.2fm, %d decals, %d entities, %d textures",
				filepath.Base(virtualPath), time.Since(parseStart),
				m.Name, m.Relief.W, m.Relief.H, m.Ground.CellsX, m.Ground.CellsZ,
				m.Ground.CellSize, len(m.Decals), len(m.Entities), len(m.TexNames))
		}
	}
	ui.OpenTabs = append(ui.OpenTabs, t)
	ui.TabSel = len(ui.OpenTabs)
	uikit.EnsureTabClicks(ui)
	if t.SgbError != "" {
		ui.StatusText = "Open " + filepath.Base(virtualPath) + " failed: " + t.SgbError
	} else {
		ui.StatusText = "Opened " + filepath.Base(virtualPath)
	}
}

// SgbToggle is one overlay switch of the viewer.
type SgbToggle struct {
	Bit   uint8
	Label string
	Click *widget.Clickable
}

// SgbToggles is the viewer's switch strip in draw order.
func SgbToggles(t *uistate.Tab) []SgbToggle {
	return []SgbToggle{
		{SgbOverTypes, "Terrain types", &t.SgbToggleTypes},
		{SgbOverPass, "Passability", &t.SgbTogglePass},
		{SgbOverDecals, "Decals", &t.SgbToggleDecals},
		{SgbOverEntities, "Entities", &t.SgbToggleEntities},
	}
}

// sgbOverlayBar draws the overlay switches: each button reads ✓ while its
// overlay is baked into the view.
func SgbOverlayBar(ui *uistate.State, gtx layout.Context, t *uistate.Tab) layout.Dimensions {
	toggles := SgbToggles(t)
	var children []layout.FlexChild
	for i := range toggles {
		if i > 0 {
			children = append(children, layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout))
		}
		tg := toggles[i]
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			mark := "· "
			if t.SgbMask&tg.Bit != 0 {
				mark = "✓ "
			}
			return uikit.SmallButton(ui, gtx, tg.Click, mark+tg.Label)
		}))
	}
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx, children...)
}

// drawSgbCanvas paints the composite map image scaled to fit the panel, the
// same affine paint the image viewer uses.
func DrawSgbCanvas(ui *uistate.State, gtx layout.Context, t *uistate.Tab) layout.Dimensions {
	area := gtx.Constraints.Max
	if area.X < 1 || area.Y < 1 {
		return layout.Dimensions{}
	}
	gtx.Constraints.Min = area
	b := t.SgbImage.Bounds()
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
			paint.ColorOp{Color: uikit.Rgb(0x303030)}.Add(gtx.Ops)
			paint.PaintOp{}.Add(gtx.Ops)
			st.Pop()
			return layout.Dimensions{Size: area}
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			st := clip.Rect{Max: area}.Push(gtx.Ops)
			defer st.Pop()
			aff := op.Affine(f32.AffineId().Scale(f32.Pt(0, 0), f32.Pt(scale, scale)).Offset(f32.Pt(float32(x)-float32(b.Min.X)*scale, float32(y)-float32(b.Min.Y)*scale)))
			tr := aff.Push(gtx.Ops)
			paint.NewImageOp(t.SgbImage).Add(gtx.Ops)
			paint.PaintOp{}.Add(gtx.Ops)
			tr.Pop()
			return layout.Dimensions{Size: area}
		}),
	)
}

// BuildSgbImage bakes the map into one NRGBA: the heightmap as a shaded grey
// relief, a faint grid every 64 cells, and the requested overlays blended on
// top — terrain-type regions, passability tint, decal dots.
func BuildSgbImage(m *sgb.Map, mask uint8) *image.NRGBA {
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
			img.Pix[o+0] = SgbClampByte(g * shade)
			img.Pix[o+1] = SgbClampByte(g * shade)
			img.Pix[o+2] = SgbClampByte(g * shade)
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
			img.Pix[o+0] = SgbClampByte(float32(img.Pix[o+0]) * 0.75)
			img.Pix[o+1] = SgbClampByte(float32(img.Pix[o+1]) * 0.75)
			img.Pix[o+2] = SgbClampByte(float32(img.Pix[o+2]) * 0.75)
		}
	}
	if mask&SgbOverTypes != 0 {
		BlendGrid(img, m.Terrain, func(v byte) (float32, float32, float32, float32) {
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
	if mask&SgbOverPass != 0 {
		BlendGrid(img, m.Pass, func(v byte) (float32, float32, float32, float32) {
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
	if mask&SgbOverDecals != 0 {
		// Decals are placed in world metres; the world header converts a
		// metre to a cell index. A map that is 128×1024 keeps its own aspect
		// because each axis uses its own ground extent.
		for _, d := range m.Decals {
			px, py := SgbWorldToCell(m, d.X, d.Z)
			// A decal's footprint is its world size, rotated on the map; a
			// dot is enough here, so the rotation only steers which cells
			// the footprint tints.
			radius := SgbDecalRadius(m, d)
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
	if mask&SgbOverEntities != 0 {
		// Entities share the decals' world frame. Each marker is a bright
		// core with a short tick along the entity's heading, so rows of
		// walls read as aligned walls while single objects stay findable.
		for _, e := range m.Entities {
			px, py := SgbWorldToCell(m, e.X, e.Z)
			SgbEntityMark(img, px, py, math.Sin(float64(e.Heading)), math.Cos(float64(e.Heading)))
		}
	}
	return img
}

// SgbEntityMark draws one placed object: soft arm pixels around a bright
// core, then a three-pixel tick pointing along the entity's heading. Every
// pixel write is bounds checked, so an entity parked at the map edge still
// draws its in-bounds half.
func SgbEntityMark(img *image.NRGBA, x, y int, dx, dy float64) {
	b := img.Bounds()
	put := func(px, py int, r, g, bl float32) {
		if px < b.Min.X || py < b.Min.Y || px >= b.Max.X || py >= b.Max.Y {
			return
		}
		o := img.PixOffset(px, py)
		img.Pix[o+0] = SgbClampByte(r)
		img.Pix[o+1] = SgbClampByte(g)
		img.Pix[o+2] = SgbClampByte(bl)
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

// SgbWorldToCell converts a world position in metres to a heightmap cell
// index, using the ground header's centre and cell size so a decal sits where
// the game would draw it.
func SgbWorldToCell(m *sgb.Map, worldX, worldZ float32) (int, int) {
	g := m.Ground
	if g.CellSize <= 0 {
		return 0, 0
	}
	cx := int(math.Round(float64(worldX)/float64(g.CellSize) + float64(g.CenterX-1)))
	cz := int(math.Round(float64(worldZ)/float64(g.CellSize) + float64(g.CenterZ-1)))
	return cx, cz
}

// SgbDecalRadius is the decal footprint in cells, from its world size. It is
// clamped so a stray size cannot tint the whole map.
func SgbDecalRadius(m *sgb.Map, d sgb.Decal) int {
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

// BlendGrid tints the composite with a smaller grid, sampling it nearest and
// blending through the color's alpha.
func BlendGrid(img *image.NRGBA, g sgb.Grid, colorFor func(v byte) (float32, float32, float32, float32)) {
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
			img.Pix[o+0] = SgbClampByte(float32(img.Pix[o+0])*(1-a) + 255*r*a)
			img.Pix[o+1] = SgbClampByte(float32(img.Pix[o+1])*(1-a) + 255*gr*a)
			img.Pix[o+2] = SgbClampByte(float32(img.Pix[o+2])*(1-a) + 255*bl*a)
		}
	}
}

// SgbClampByte is the viewer's float-to-byte clamp; it is separate from the
// fx overlay's float64 engine.ClampByte because all the relief maths here is float32.
func SgbClampByte(v float32) byte {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return byte(v + 0.5)
}

// PaintSgbSelection draws the software fallback's selection bounds and keeps
// the drag marquee in the UI. Vulkan draws selection bounds into its color target.
func PaintSgbSelection(gtx layout.Context, tab *uistate.Tab, mv *viewer.ModelViewer, width, height int, drawBounds bool) {
	area := image.Rectangle{Max: image.Pt(width, height)}
	canvasClip := clip.Rect{Max: area.Max}.Push(gtx.Ops)
	defer canvasClip.Pop()
	if tab == nil || mv == nil || tab.SgbMap == nil || width <= 0 || height <= 0 {
		return
	}
	if drawBounds && mv.Model != nil && len(tab.SgbSelectedEntities) > 0 {
		pose := viewer.Pose(mv)
		if len(mv.AnimatedEntities) > 0 {
			pose = sgb.CombineEntityPoses(len(pose.Bones), mv.AnimatedEntities)
		}
		bounds := render.SgbEntityProjectedBounds(viewer.ModelData(mv), tab.SgbEntityMeshIDs, render.BuildViewMatrix(viewer.Options(mv, width, height)), width, height, pose, len(pose.Bones) > 0)
		for id := range tab.SgbSelectedEntities {
			box := bounds[id].Inset(-3).Intersect(area)
			if !box.Empty() {
				paint.FillShape(gtx.Ops, color.NRGBA{R: 255, G: 220, B: 80, A: 220}, clip.Stroke{Path: SgbRectPath(gtx, box), Width: 2}.Op())
			}
		}
	}
	PaintSgbSelectionMarquee(gtx, tab, area)
}

func PaintSgbSelectionMarquee(gtx layout.Context, tab *uistate.Tab, area image.Rectangle) {
	if tab == nil || !tab.SgbSelecting || !tab.SgbSelectionDrag {
		return
	}
	box := SgbSelectionRect(tab.SgbSelectionAnchor, tab.SgbSelectionCurrent).Intersect(area)
	if box.Empty() {
		return
	}
	paint.FillShape(gtx.Ops, color.NRGBA{R: 90, G: 170, B: 255, A: 30}, clip.Rect{Min: box.Min, Max: box.Max}.Op())
	paint.FillShape(gtx.Ops, color.NRGBA{R: 110, G: 190, B: 255, A: 230}, clip.Stroke{Path: SgbRectPath(gtx, box), Width: 1}.Op())
}

// SgbCanvasToFramePoint undoes the frame's scale and letterboxing so pointer
// coordinates and projected scene pixels share one coordinate system.
func SgbCanvasToFramePoint(area image.Point, frameWidth, frameHeight int, point f32.Point) image.Point {
	if area.X <= 0 || area.Y <= 0 || frameWidth <= 0 || frameHeight <= 0 {
		return image.Point{}
	}
	scale := math.Min(float64(area.X)/float64(frameWidth), float64(area.Y)/float64(frameHeight))
	offsetX := (float64(area.X) - float64(frameWidth)*scale) / 2
	offsetY := (float64(area.Y) - float64(frameHeight)*scale) / 2
	x := int(math.Round((float64(point.X) - offsetX) / scale))
	y := int(math.Round((float64(point.Y) - offsetY) / scale))
	return image.Pt(uikit.MaxInt(x, 0), uikit.MaxInt(y, 0))
}

func SgbPickEntity(model *whm.Model, entityMeshes map[string]int, camera engine.Camera, pixelX, pixelY float64) (int, bool) {
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
				vertices[j] = render.VecFromWhm(model.Vertices[index].Pos)
			}
			if distance, hit := SgbRayTriangle(origin, direction, vertices[0], vertices[1], vertices[2]); hit && distance < closest {
				closest, picked = distance, id
			}
		}
	}
	return picked, !math.IsInf(closest, 1)
}

func SgbRectangleEntities(model *whm.Model, entityMeshes map[string]int, view fauxgl.Matrix, width, height int, rect image.Rectangle) map[int]bool {
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
				projected := view.MulPositionW(render.VecFromWhm(model.Vertices[index].Pos))
				if projected.W <= 1e-6 {
					valid = false
					break
				}
				x := (projected.X/projected.W*0.5 + 0.5) * float64(width)
				y := (1 - (projected.Y/projected.W*0.5 + 0.5)) * float64(height)
				points[j] = image.Pt(int(math.Round(x)), int(math.Round(y)))
			}
			if valid && SgbTriangleIntersectsRect(points, rect) {
				selected[id] = true
				break
			}
		}
	}
	return selected
}

func SgbTriangleIntersectsRect(triangle [3]image.Point, rect image.Rectangle) bool {
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
		if SgbPointInTriangle(corner, triangle) {
			return true
		}
	}
	for edge := 0; edge < 3; edge++ {
		a, b := triangle[edge], triangle[(edge+1)%3]
		for side := 0; side < 4; side++ {
			c, d := corners[side], corners[(side+1)%4]
			if SgbSegmentsIntersect(a, b, c, d) {
				return true
			}
		}
	}
	return false
}

func SgbPointInTriangle(point image.Point, triangle [3]image.Point) bool {
	orientation := func(a, b, p image.Point) int64 {
		return int64(b.X-a.X)*int64(p.Y-a.Y) - int64(b.Y-a.Y)*int64(p.X-a.X)
	}
	d0 := orientation(triangle[0], triangle[1], point)
	d1 := orientation(triangle[1], triangle[2], point)
	d2 := orientation(triangle[2], triangle[0], point)
	return !(d0 < 0 || d1 < 0 || d2 < 0) || !(d0 > 0 || d1 > 0 || d2 > 0)
}

func SgbSegmentsIntersect(a, b, c, d image.Point) bool {
	orientation := func(p, q, r image.Point) int64 {
		return int64(q.X-p.X)*int64(r.Y-p.Y) - int64(q.Y-p.Y)*int64(r.X-p.X)
	}
	onSegment := func(p, q, r image.Point) bool {
		return q.X >= uikit.MinInt(p.X, r.X) && q.X <= uikit.MaxInt(p.X, r.X) && q.Y >= uikit.MinInt(p.Y, r.Y) && q.Y <= uikit.MaxInt(p.Y, r.Y)
	}
	o1, o2, o3, o4 := orientation(a, b, c), orientation(a, b, d), orientation(c, d, a), orientation(c, d, b)
	if (o1 < 0 && o2 > 0 || o1 > 0 && o2 < 0) && (o3 < 0 && o4 > 0 || o3 > 0 && o4 < 0) {
		return true
	}
	return o1 == 0 && onSegment(a, c, b) || o2 == 0 && onSegment(a, d, b) || o3 == 0 && onSegment(c, a, d) || o4 == 0 && onSegment(c, b, d)
}

func SgbSelectionRect(a, b image.Point) image.Rectangle {
	return image.Rect(uikit.MinInt(a.X, b.X), uikit.MinInt(a.Y, b.Y), uikit.MaxInt(a.X, b.X), uikit.MaxInt(a.Y, b.Y))
}

func SgbRectPath(gtx layout.Context, rect image.Rectangle) clip.PathSpec {
	var path clip.Path
	path.Begin(gtx.Ops)
	path.MoveTo(f32.Pt(float32(rect.Min.X), float32(rect.Min.Y)))
	path.LineTo(f32.Pt(float32(rect.Max.X), float32(rect.Min.Y)))
	path.LineTo(f32.Pt(float32(rect.Max.X), float32(rect.Max.Y)))
	path.LineTo(f32.Pt(float32(rect.Min.X), float32(rect.Max.Y)))
	path.Close()
	return path.End()
}
