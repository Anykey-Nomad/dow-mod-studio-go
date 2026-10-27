package sgb

import (
	"dow-mod-studio/internal/app/render"
	"dow-mod-studio/internal/app/uikit"
	"dow-mod-studio/internal/app/uistate"
	"dow-mod-studio/internal/app/viewer"
	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/raster"
	"dow-mod-studio/internal/sgb"
	"dow-mod-studio/internal/whm"
	"image"
	"image/draw"
	"log"
	"math"
	"strings"
	"time"
)

const (
	// SgbCameraDistance is how far the 3D map view's camera opens from the
	// ground, in metres: the range the in-game camera works at, rather than
	// the whole-map fit a model viewer would choose.
	SgbCameraDistance = 70.0
	// SgbCameraMinDistance is the closest the wheel may bring the camera,
	// still an orbit a unit would fit in rather than a pinch of ground.
	SgbCameraMinDistance = 10.0
	// SgbCameraPitch is the opening elevation — what the game's camera looks
	// down from. The model viewer's 0.2 rad is nearly sideways at map scale.
	SgbCameraPitch = 49.481994013136 * math.Pi / 180
	// The opening view's camera-to-axis angles are the closest realizable
	// direction to X=120°, Y=65°, Z=140° for a single unit-length vector.
	SgbCameraYaw = -40.205715405573 * math.Pi / 180
)

// SgbCameraDefaultDistance picks the opening camera distance: the in-game
// range, but never past the fit of a map too small to need it.
func SgbCameraDefaultDistance(m *sgb.Map) float64 {
	if m == nil {
		return SgbCameraDistance
	}
	w := float64(m.Ground.XMax() - m.Ground.XMin())
	d := float64(m.Ground.ZMax() - m.Ground.ZMin())
	fit := 1.4 * math.Hypot(w, d)
	if fit > 0 && fit < SgbCameraDistance {
		return fit
	}
	return SgbCameraDistance
}

// SgbTerrainMaxGrid caps one axis of the terrain grid. The hardware
// renderer's index buffer is 16 bit and buildMesh refuses a model above
// 0xffff vertices, so a 250×250 grid is the densest one both paths draw.
const SgbTerrainMaxGrid = 250

// SgbTerrainMaterial names one map's terrain diffuse. It carries the tab's
// path so two open maps never share a texture slot in the renderer's cache.
func SgbTerrainMaterial(virtualPath string) string {
	return "sgb-terrain:" + virtualPath
}

// SgbTerrainModel wraps the scenario package's heightmap mesh for the
// viewer's model type.
func SgbTerrainModel(m *sgb.Map, material string) *uistate.WhmModel {
	model := m.TerrainModel(SgbTerrainMaxGrid)
	if model == nil {
		return nil
	}
	model.Meshes[0].Material = material
	m.AddWaterMesh(model)
	return &uistate.WhmModel{Model: *model}
}

// SgbTerrainHeightAt returns the heightmap's bilinearly interpolated surface
// height at world-space X/Y coordinates, clamped to the map edges.
func SgbTerrainHeightAt(m *sgb.Map, x, y float64) (float64, bool) {
	if m == nil || m.Relief.W < 2 || m.Relief.H < 2 || len(m.Relief.Data) < m.Relief.W*m.Relief.H ||
		m.Ground.CellSize <= 0 {
		return 0, false
	}
	cell := float64(m.Ground.CellSize)
	ix := math.Max(0, math.Min(float64(m.Relief.W-1), x/cell+float64(m.Ground.CenterX)-1))
	iy := math.Max(0, math.Min(float64(m.Relief.H-1), y/cell+float64(m.Ground.CenterZ)-1))
	x0, y0 := int(math.Floor(ix)), int(math.Floor(iy))
	x1, y1 := uikit.MinInt(x0+1, m.Relief.W-1), uikit.MinInt(y0+1, m.Relief.H-1)
	fx, fy := ix-float64(x0), iy-float64(y0)
	h00 := float64(m.Relief.At(x0, y0))
	h10 := float64(m.Relief.At(x1, y0))
	h01 := float64(m.Relief.At(x0, y1))
	h11 := float64(m.Relief.At(x1, y1))
	h0 := h00*(1-fx) + h10*fx
	h1 := h01*(1-fx) + h11*fx
	return (h0*(1-fy) + h1*fy) * float64(m.Ground.HeightScale), true
}

// SgbApproachCameraVelocity eases one movement axis toward its input target.
// A linear ramp gives predictable acceleration and a soft stop when released.
func SgbApproachCameraVelocity(value, target, seconds float64) float64 {
	const acceleration = 4.5 // reach full movement speed in about 220 ms
	step := acceleration * seconds
	if value < target {
		value = math.Min(value+step, target)
	} else {
		value = math.Max(value-step, target)
	}
	if target == 0 && math.Abs(value) < 1e-4 {
		return 0
	}
	return value
}

// sgbMoveCamera advances the orbit target over the map in camera-relative
// directions. forward and strafe are signed velocity axes; seconds makes
// motion independent of frame rate. The target's Z follows the terrain.
func SgbMoveCamera(ui *uistate.State, t *uistate.Tab, forward, strafe, seconds float64) bool {
	if t == nil || t.SgbMap == nil || t.Model == nil || seconds <= 0 || (forward == 0 && strafe == 0) {
		return false
	}
	length := math.Hypot(forward, strafe)
	if length > 1 {
		forward, strafe = forward/length, strafe/length
	}
	speed := math.Max(float64(t.SgbMap.Ground.CellSize)*8, 20) // metres per second
	forwardX, forwardY := -math.Cos(t.Model.Yaw), -math.Sin(t.Model.Yaw)
	rightX, rightY := -forwardY, forwardX
	distance := speed * seconds
	dx := (forwardX*forward + rightX*strafe) * distance
	dy := (forwardY*forward + rightY*strafe) * distance
	g := t.SgbMap.Ground
	x := math.Max(float64(g.XMin()), math.Min(float64(g.XMax()), t.Model.Center.X+dx))
	y := math.Max(float64(g.ZMin()), math.Min(float64(g.ZMax()), t.Model.Center.Y+dy))
	if x == t.Model.Center.X && y == t.Model.Center.Y {
		return false
	}
	height, ok := SgbTerrainHeightAt(t.SgbMap, x, y)
	if !ok {
		return false
	}
	t.Model.Center.X, t.Model.Center.Y, t.Model.Center.Z = x, y, height
	t.Model.ActiveCamera = ""
	viewer.Invalidate(t.Model)
	return true
}

const (
	// Bound the texture uploaded to the GPU. Native tile pixels and repeat
	// count determine requested resolution; this cap only applies to large maps.
	SgbTerrainTextureMaxDim    = 2048
	SgbTerrainTextureMaxPixels = 4 * 1024 * 1024
)

// SgbBuildTerrainTexture composes the DSHD ground tile assets and placed
// SMAP decals. Its diffuse resolution is asset-driven, not tied to heightmap
// resolution or the map's UI preview image.
func SgbBuildTerrainTexture(tree *modtree.ModTree, _ string, m *sgb.Map) *image.RGBA {
	if m == nil {
		return nil
	}
	start := time.Now()
	source := "tiles"
	base := SgbBuildTileTexture(tree, m)
	if base == nil {
		source = "relief fallback"
		base = BuildSgbImage(m, 0)
	}
	if base == nil {
		return nil
	}
	decals := SgbBakeDecals(base, tree, m)
	b := base.Bounds()
	log.Printf("sgb: terrain texture %s: %dx%d, %d decals baked, in %v",
		source, b.Dx(), b.Dy(), decals, time.Since(start))
	out := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(out, out.Bounds(), base, b.Min, draw.Src)
	return out
}

// SgbBuildTileTexture uses native texture detail: each map tile gets
// TileRepeat repeats of its texture at its source dimensions. The safety cap
// only reduces a massive result; it never selects detail from map dimensions.
func SgbBuildTileTexture(tree *modtree.ModTree, m *sgb.Map) *image.NRGBA {
	if tree == nil || m == nil || m.Ground.CellsX <= 0 || m.Ground.CellsZ <= 0 ||
		m.Ground.TileSize <= 0 || m.TileNX <= 0 || m.TileNZ <= 0 || len(m.Tiles) == 0 {
		return nil
	}
	repeat := uikit.MaxInt(1, m.Ground.TileRepeat)
	// Resolve each source once and size each tile from the native image. Each
	// tile requests native texture size × repeat; only a large final image is
	// uniformly reduced to the GPU safety cap.
	cache := make(map[int]*image.NRGBA, len(m.TexNames))
	tileWidth, tileHeight := 0, 0
	for _, ptr := range m.Tiles {
		if _, seen := cache[ptr]; seen {
			continue
		}
		tex := SgbTreeTexture(tree, m.TexNames[ptr])
		cache[ptr] = tex
		if tex != nil {
			if tex.Bounds().Dx() > tileWidth {
				tileWidth = tex.Bounds().Dx()
			}
			if tex.Bounds().Dy() > tileHeight {
				tileHeight = tex.Bounds().Dy()
			}
		}
	}
	if tileWidth < 1 || tileHeight < 1 {
		return nil
	}
	width := m.TileNX * tileWidth * repeat
	height := m.TileNZ * tileHeight * repeat
	if width < 1 || height < 1 {
		return nil
	}
	scale := math.Min(1, math.Min(float64(SgbTerrainTextureMaxDim)/float64(width), float64(SgbTerrainTextureMaxDim)/float64(height)))
	if pixels := int64(width) * int64(height); pixels > SgbTerrainTextureMaxPixels {
		scale = math.Min(scale, math.Sqrt(float64(SgbTerrainTextureMaxPixels)/float64(pixels)))
	}
	width = uikit.MaxInt(1, int(math.Round(float64(width)*scale)))
	height = uikit.MaxInt(1, int(math.Round(float64(height)*scale)))
	tilePixelWidth := width / m.TileNX
	tilePixelHeight := height / m.TileNZ
	if tilePixelWidth < 1 || tilePixelHeight < 1 {
		return nil
	}
	out := image.NewNRGBA(image.Rect(0, 0, width, height))
	// Start transparent. Missing tile references are not replaced by the UI
	// preview TGA or by stretching a low-resolution relief image.
	painted := 0
	for tz := 0; tz < m.TileNZ; tz++ {
		for tx := 0; tx < m.TileNX; tx++ {
			idx := tz*m.TileNX + tx
			if idx >= len(m.Tiles) {
				continue
			}
			tex := cache[m.Tiles[idx]]
			if tex == nil {
				continue
			}
			x0 := tx * width / m.TileNX
			x1 := (tx + 1) * width / m.TileNX
			y0 := tz * height / m.TileNZ
			y1 := (tz + 1) * height / m.TileNZ
			if x1 <= x0 || y1 <= y0 {
				continue
			}
			for y := y0; y < y1; y++ {
				v := 1 - float64(y-y0)/float64(y1-y0)*float64(repeat)
				for x := x0; x < x1; x++ {
					u := float64(x-x0) / float64(x1-x0) * float64(repeat)
					r, g, b, a := SgbSample(tex, u, v, true)
					if a > 0 {
						SgbBlendPixel(out, x, y, r, g, b, a)
					}
				}
			}
			painted++
		}
	}
	if painted == 0 {
		return nil
	}
	log.Printf("sgb: terrain diffuse sampled from %d/%d painted tiles at %dx%d", painted, m.TileNX*m.TileNZ, width, height)
	return out
}

// SgbEntityMaterials resolves the .rsh diffuse maps of every material a
// scene's object meshes name, keeping the materials already resolved (the
// terrain's own diffuse). It returns raw bytes for Vulkan, the merged material
// map, the count of missing textures, and source/format diagnostics.
func SgbEntityMaterials(tree *modtree.ModTree, sceneFile string, scene *whm.Model, keep string, base map[string]render.WhmMaterial) (map[string][]byte, map[string]render.WhmMaterial, int, map[string]viewer.ModelMaterialDiagnostic) {
	if base == nil {
		base = make(map[string]render.WhmMaterial)
	}
	needed := map[string]bool{}
	for _, mesh := range scene.Meshes {
		if mesh.Material == "" || mesh.Material == keep || mesh.Material == sgb.WaterMaterialName {
			continue
		}
		// The shared shader's diffuse reference is the useful lookup key;
		// meshes still receive the decoded result under mesh.Material.
		key := mesh.Material
		if mesh.MaterialPath != "" {
			key = mesh.MaterialPath
		}
		needed[key] = true
	}
	if len(needed) == 0 {
		return nil, base, 0, map[string]viewer.ModelMaterialDiagnostic{}
	}
	names := make([]string, 0, len(needed))
	for name := range needed {
		names = append(names, name)
	}
	files, paths := viewer.ResolveModelMaterialsWithPaths(tree, viewer.MaterialSearchRoots(tree), sceneFile, names)
	res := viewer.DecodeModelMaterials(files, true)
	viewer.AttachMaterialSourcePaths(res.Diagnostics, paths)
	for _, mesh := range scene.Meshes {
		if mesh.Material == "" || mesh.Material == keep || mesh.Material == sgb.WaterMaterialName {
			continue
		}
		key := mesh.Material
		if mesh.MaterialPath != "" {
			key = mesh.MaterialPath
		}
		if mat, ok := res.Materials[key]; ok {
			base[mesh.Material] = mat
		}
		if raw, ok := res.Rsh[key]; ok {
			res.Rsh[mesh.Material] = raw
		}
		if diag, ok := res.Diagnostics[key]; ok {
			diag.ResolvedPath = paths[key]
			res.Diagnostics[mesh.Material] = diag
		}
	}
	for name, mat := range res.Materials {
		base[name] = mat
	}
	for _, name := range names {
		if _, ok := res.Diagnostics[name]; !ok {
			res.Diagnostics[name] = viewer.ModelMaterialDiagnostic{Format: "—", Status: "not found"}
		}
	}
	return res.Rsh, base, res.Missing, res.Diagnostics
}

// SgbBlendTiles paints tile textures over a relief-sized preview image. Kept
// for callers which explicitly request a relief-sized diagnostic image; the
// 3D path instead uses SgbBuildTileTexture at asset-driven resolution.
func SgbBlendTiles(dst *image.NRGBA, tree *modtree.ModTree, m *sgb.Map) {
	g := m.Ground
	if dst == nil || g.CellsX <= 0 || g.CellsZ <= 0 || g.TileSize <= 0 || m.TileNX <= 0 || m.TileNZ <= 0 {
		return
	}
	b := dst.Bounds()
	ppcX := float64(b.Dx()) / float64(g.CellsX)
	ppcZ := float64(b.Dy()) / float64(g.CellsZ)
	repeat := float64(g.TileRepeat)
	if repeat <= 0 {
		repeat = 1
	}
	cache := map[string]*image.NRGBA{}
	painted := 0
	for tz := 0; tz < m.TileNZ; tz++ {
		for tx := 0; tx < m.TileNX; tx++ {
			idx := tz*m.TileNX + tx
			if idx >= len(m.Tiles) {
				continue
			}
			name := m.TexNames[m.Tiles[idx]]
			if name == "" {
				continue
			}
			tex := cache[name]
			if _, seen := cache[name]; !seen {
				tex = SgbTreeTexture(tree, name)
				cache[name] = tex
			}
			if tex == nil {
				continue
			}
			x0 := int(float64(tx*g.TileSize)*ppcX + 0.5)
			x1 := int(float64((tx+1)*g.TileSize)*ppcX + 0.5)
			z0 := int(float64(tz*g.TileSize)*ppcZ + 0.5)
			z1 := int(float64((tz+1)*g.TileSize)*ppcZ + 0.5)
			if x1 > b.Max.X {
				x1 = b.Max.X
			}
			if z1 > b.Max.Y {
				z1 = b.Max.Y
			}
			if x1-x0 < 1 || z1-z0 < 1 {
				continue
			}
			for py := z0; py < z1; py++ {
				// The editor ramps its tile coordinate 0..repeat over
				// the tile and samples the buffer bottom-up, hence the
				// flipped v into the decoded (top-down) image.
				v := 1 - (float64(py)-float64(z0))/float64(z1-z0)*repeat
				for px := x0; px < x1; px++ {
					u := (float64(px) - float64(x0)) / float64(x1-x0) * repeat
					r, gr, bl, a := SgbSample(tex, u, v, true)
					if a <= 0 {
						continue
					}
					SgbBlendPixel(dst, px, py, r, gr, bl, a)
				}
			}
			painted++
		}
	}
	if painted > 0 {
		log.Printf("sgb: relief base blended with %d tile textures", painted)
	}
}

// SgbBakeDecals draws every placed decal onto the base and reports how many
// landed. A decal whose texture cannot be resolved is skipped, and a map with
// thousands of them still bakes in one pass — the records share textures
// through the SMAP table, so the decode count is the table's size, not the
// placement count.
func SgbBakeDecals(dst *image.NRGBA, tree *modtree.ModTree, m *sgb.Map) int {
	if dst == nil || m == nil || len(m.Decals) == 0 {
		return 0
	}
	cache := map[int]*image.NRGBA{}
	baked, missing := 0, 0
	for _, d := range m.Decals {
		tex, seen := cache[d.Ptr]
		if !seen {
			tex = SgbTreeTexture(tree, d.Name)
			if tex == nil {
				// The game's special baddecal asset is intentionally not
				// used: it is a solid square diagnostic, not a real decal.
				missing++
			}
			cache[d.Ptr] = tex
		}
		if tex == nil {
			continue
		}
		if SgbDrawDecal(dst, m, d, tex) {
			baked++
		}
	}
	if missing > 0 {
		log.Printf("sgb: %d decal textures not found in the mod tree", missing)
	}
	return baked
}

// SgbDrawDecal alpha blends one decal's texture onto the base. The footprint
// is texW*size × texH*size metres and the rotated mapping is the same matrix
// the reference editor applies to the decal texture coordinates.
func SgbDrawDecal(dst *image.NRGBA, m *sgb.Map, d sgb.Decal, tex *image.NRGBA) bool {
	g := m.Ground
	if dst == nil || tex == nil || g.CellSize <= 0 || d.Size <= 0 {
		return false
	}
	b := dst.Bounds()
	tb := tex.Bounds()
	tw, th := float64(tb.Dx()), float64(tb.Dy())
	size := float64(d.Size)
	sx := 1 / (tw * size)
	sz := 1 / (th * size)
	// The editor measures its rotation as cos(a) and sin(-a).
	cosA := math.Cos(float64(d.Angle))
	sinA := -math.Sin(float64(d.Angle))
	xc, zc := float64(d.X), float64(d.Z)
	ppcX := float64(b.Dx()) / float64(g.CellsX)
	ppcZ := float64(b.Dy()) / float64(g.CellsZ)
	ppmX := ppcX / float64(g.CellSize)
	ppmZ := ppcZ / float64(g.CellSize)
	// Half the rotated footprint in metres, as the editor's index bounds.
	halfX := (math.Abs(cosA)/sx + math.Abs(sinA)/sz) / 2
	halfZ := (math.Abs(sinA)/sx + math.Abs(cosA)/sz) / 2
	centerPX := (xc/float64(g.CellSize) + float64(g.CenterX-1)) * ppcX
	centerPZ := (zc/float64(g.CellSize) + float64(g.CenterZ-1)) * ppcZ
	x0 := uikit.ClampInt(int(math.Floor(centerPX-halfX*ppmX)), 0, b.Dx()-1)
	x1 := uikit.ClampInt(int(math.Ceil(centerPX+halfX*ppmX)), 0, b.Dx())
	z0 := uikit.ClampInt(int(math.Floor(centerPZ-halfZ*ppmZ)), 0, b.Dy()-1)
	z1 := uikit.ClampInt(int(math.Ceil(centerPZ+halfZ*ppmZ)), 0, b.Dy())
	if x1-x0 < 1 || z1-z0 < 1 {
		return false
	}
	for py := z0; py < z1; py++ {
		worldZ := ((float64(py)+0.5)/ppcZ - float64(g.CenterZ-1)) * float64(g.CellSize)
		dz := worldZ - zc
		for px := x0; px < x1; px++ {
			worldX := ((float64(px)+0.5)/ppcX - float64(g.CenterX-1)) * float64(g.CellSize)
			dxu := worldX - xc
			u := sx*(cosA*dxu+sinA*dz) + 0.5
			v := sz*(sinA*dxu+cosA*dz) + 0.5
			if u < 0 || u >= 1 || v < 0 || v >= 1 {
				continue
			}
			// GL samples the buffer bottom-up; the decoded image is
			// top-down, hence the flipped v.
			r, gr, bl, a := SgbSample(tex, u, 1-v, false)
			if a <= 0 {
				continue
			}
			SgbBlendPixel(dst, px, py, r, gr, bl, a)
		}
	}
	return true
}

// SgbTreeTexture loads a map-referenced texture — a DSHD or SMAP blueprint
// path with no extension, relative to Data — trying .tga first and .dds the
// way the reference editor does.
func SgbTreeTexture(tree *modtree.ModTree, name string) *image.NRGBA {
	if tree == nil || name == "" {
		return nil
	}
	rel := strings.ReplaceAll(strings.TrimSpace(name), "\\", "/")
	rel = strings.TrimPrefix(rel, "/")
	if !strings.EqualFold(rel[:uikit.MinInt(len(rel), 5)], "data/") {
		rel = "Data/" + rel
	}
	for _, ext := range []string{".tga", ".dds"} {
		data, err := tree.ReadFile(rel + ext)
		if err != nil {
			continue
		}
		img, _, err := raster.DecodeImageData(data, strings.TrimPrefix(ext, "."))
		if err != nil || img == nil {
			continue
		}
		out := image.NewNRGBA(image.Rect(0, 0, img.Bounds().Dx(), img.Bounds().Dy()))
		draw.Draw(out, out.Bounds(), img, img.Bounds().Min, draw.Src)
		return out
	}
	return nil
}

// SgbSample bilinearly samples a decoded texture at (u, v) in 0..1, either
// clamped to the edge or wrapped — the tile textures repeat, the decal ones
// clamp.
func SgbSample(img *image.NRGBA, u, v float64, wrap bool) (float64, float64, float64, float64) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w < 1 || h < 1 {
		return 0, 0, 0, 0
	}
	if wrap {
		u -= math.Floor(u)
		v -= math.Floor(v)
	} else {
		u = math.Max(0, math.Min(1, u))
		v = math.Max(0, math.Min(1, v))
	}
	x := u*float64(w) - 0.5
	y := v*float64(h) - 0.5
	x0 := math.Floor(x)
	y0 := math.Floor(y)
	fx, fy := x-x0, y-y0
	var acc [4]float64
	for _, tap := range [4][2]float64{{0, 0}, {1, 0}, {0, 1}, {1, 1}} {
		ix, iy := int(x0)+int(tap[0]), int(y0)+int(tap[1])
		if wrap {
			ix = ((ix % w) + w) % w
			iy = ((iy % h) + h) % h
		} else {
			ix = uikit.ClampInt(ix, 0, w-1)
			iy = uikit.ClampInt(iy, 0, h-1)
		}
		weight := (1 - math.Abs(float64(int(tap[0]))-fx)) * (1 - math.Abs(float64(int(tap[1]))-fy))
		o := img.PixOffset(b.Min.X+ix, b.Min.Y+iy)
		a := float64(img.Pix[o+3]) / 255
		// Interpolate premultiplied colour. Filtering straight RGB lets
		// the arbitrary RGB stored beneath transparent decal texels bleed
		// into visible edge pixels, producing white square halos.
		acc[0] += weight * float64(img.Pix[o+0]) * a
		acc[1] += weight * float64(img.Pix[o+1]) * a
		acc[2] += weight * float64(img.Pix[o+2]) * a
		acc[3] += weight * float64(img.Pix[o+3])
	}
	if acc[3] <= 0 {
		return 0, 0, 0, 0
	}
	// RGB was accumulated as byte-valued colour times normalized alpha,
	// while acc[3] is byte-valued alpha. Their ratio is already normalized
	// RGB; dividing it by 255 again reduces every tile to near-black.
	return acc[0] / acc[3], acc[1] / acc[3], acc[2] / acc[3], acc[3] / 255
}

// SgbBlendPixel source-overs one normalized sampled colour into the byte
// channels of the base, which stays opaque throughout.
func SgbBlendPixel(dst *image.NRGBA, x, y int, r, g, b, a float64) {
	o := dst.PixOffset(x, y)
	dst.Pix[o+0] = SgbClampByte(float32(r*a*255 + float64(dst.Pix[o+0])*(1-a)))
	dst.Pix[o+1] = SgbClampByte(float32(g*a*255 + float64(dst.Pix[o+1])*(1-a)))
	dst.Pix[o+2] = SgbClampByte(float32(b*a*255 + float64(dst.Pix[o+2])*(1-a)))
	dst.Pix[o+3] = 0xFF
}
