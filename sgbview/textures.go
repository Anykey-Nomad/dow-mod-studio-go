package sgbview

import (
	"image"
	"image/draw"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strings"

	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/raster"
	"dow-mod-studio/internal/sgb"
)

const (
	terrainTextureMaxDim    = 2048
	terrainTextureMaxPixels = 4 * 1024 * 1024
)

// terrainHeightAt вычисляет высоту рельефа по билинейной интерполяции в точке (x, y).
func terrainHeightAt(m *sgb.Map, x, y float64) (float64, bool) {
	if m == nil || m.Relief.W < 2 || m.Relief.H < 2 || len(m.Relief.Data) < m.Relief.W*m.Relief.H ||
		m.Ground.CellSize <= 0 {
		return 0, false
	}
	cell := float64(m.Ground.CellSize)
	ix := math.Max(0, math.Min(float64(m.Relief.W-1), x/cell+float64(m.Ground.CenterX)-1))
	iy := math.Max(0, math.Min(float64(m.Relief.H-1), y/cell+float64(m.Ground.CenterZ)-1))
	x0, y0 := int(math.Floor(ix)), int(math.Floor(iy))
	x1, y1 := minInt(x0+1, m.Relief.W-1), minInt(y0+1, m.Relief.H-1)
	fx, fy := ix-float64(x0), iy-float64(y0)
	h00 := float64(m.Relief.At(x0, y0))
	h10 := float64(m.Relief.At(x1, y0))
	h01 := float64(m.Relief.At(x0, y1))
	h11 := float64(m.Relief.At(x1, y1))
	h0 := h00*(1-fx) + h10*fx
	h1 := h01*(1-fx) + h11*fx
	return (h0*(1-fy) + h1*fy) * float64(m.Ground.HeightScale), true
}

// buildTerrainTexture собирает диффузную текстуру земли из DSHD-тайлов и SMAP-декалей.
func buildTerrainTexture(tree *modtree.ModTree, m *sgb.Map) *image.RGBA {
	if m == nil {
		return nil
	}
	base := buildTileTexture(tree, m)
	if base == nil {
		base = buildReliefImage(m)
	}
	if base == nil {
		return nil
	}
	bakeDecals(base, tree, m)
	b := base.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(out, out.Bounds(), base, b.Min, draw.Src)
	return out
}

func buildTileTexture(tree *modtree.ModTree, m *sgb.Map) *image.NRGBA {
	if tree == nil || m == nil || m.Ground.CellsX <= 0 || m.Ground.CellsZ <= 0 ||
		m.Ground.TileSize <= 0 || m.TileNX <= 0 || m.TileNZ <= 0 || len(m.Tiles) == 0 {
		return nil
	}
	repeat := maxInt(1, m.Ground.TileRepeat)
	cache := make(map[int]*image.NRGBA, len(m.TexNames))
	tileWidth, tileHeight := 0, 0
	for _, ptr := range m.Tiles {
		if _, seen := cache[ptr]; seen {
			continue
		}
		tex := treeTexture(tree, m.TexNames[ptr])
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
	scale := math.Min(1, math.Min(float64(terrainTextureMaxDim)/float64(width), float64(terrainTextureMaxDim)/float64(height)))
	if pixels := int64(width) * int64(height); pixels > terrainTextureMaxPixels {
		scale = math.Min(scale, math.Sqrt(float64(terrainTextureMaxPixels)/float64(pixels)))
	}
	width = maxInt(1, int(math.Round(float64(width)*scale)))
	height = maxInt(1, int(math.Round(float64(height)*scale)))
	tilePixelWidth := width / m.TileNX
	tilePixelHeight := height / m.TileNZ
	if tilePixelWidth < 1 || tilePixelHeight < 1 {
		return nil
	}
	out := image.NewNRGBA(image.Rect(0, 0, width, height))
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
					r, g, b, a := sampleTexture(tex, u, v, true)
					if a > 0 {
						blendPixel(out, x, y, r, g, b, a)
					}
				}
			}
			painted++
		}
	}
	if painted == 0 {
		return nil
	}
	return out
}

func buildReliefImage(m *sgb.Map) *image.NRGBA {
	g := m.Relief
	if g.W < 2 || g.H < 2 || len(g.Data) < g.W*g.H {
		return nil
	}
	w, h := g.W, g.H
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
			gr := (35 + 165*t) * shade
			o := img.PixOffset(x, y)
			c := clampByte(gr)
			img.Pix[o+0] = c
			img.Pix[o+1] = c
			img.Pix[o+2] = c
			img.Pix[o+3] = 0xFF
		}
	}
	return img
}

func bakeDecals(dst *image.NRGBA, tree *modtree.ModTree, m *sgb.Map) int {
	if dst == nil || m == nil || len(m.Decals) == 0 {
		return 0
	}
	cache := map[int]*image.NRGBA{}
	baked := 0
	for _, d := range m.Decals {
		tex, seen := cache[d.Ptr]
		if !seen {
			tex = treeTexture(tree, d.Name)
			cache[d.Ptr] = tex
		}
		if tex == nil {
			continue
		}
		if drawDecal(dst, m, d, tex) {
			baked++
		}
	}
	return baked
}

func drawDecal(dst *image.NRGBA, m *sgb.Map, d sgb.Decal, tex *image.NRGBA) bool {
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
	cosA := math.Cos(float64(d.Angle))
	sinA := -math.Sin(float64(d.Angle))
	xc, zc := float64(d.X), float64(d.Z)
	ppcX := float64(b.Dx()) / float64(g.CellsX)
	ppcZ := float64(b.Dy()) / float64(g.CellsZ)
	ppmX := ppcX / float64(g.CellSize)
	ppmZ := ppcZ / float64(g.CellSize)
	halfX := (math.Abs(cosA)/sx + math.Abs(sinA)/sz) / 2
	halfZ := (math.Abs(sinA)/sx + math.Abs(cosA)/sz) / 2
	centerPX := (xc/float64(g.CellSize) + float64(g.CenterX-1)) * ppcX
	centerPZ := (zc/float64(g.CellSize) + float64(g.CenterZ-1)) * ppcZ
	x0 := clampInt(int(math.Floor(centerPX-halfX*ppmX)), 0, b.Dx()-1)
	x1 := clampInt(int(math.Ceil(centerPX+halfX*ppmX)), 0, b.Dx())
	z0 := clampInt(int(math.Floor(centerPZ-halfZ*ppmZ)), 0, b.Dy()-1)
	z1 := clampInt(int(math.Ceil(centerPZ+halfZ*ppmZ)), 0, b.Dy())
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
			r, gr, bl, a := sampleTexture(tex, u, 1-v, false)
			if a <= 0 {
				continue
			}
			blendPixel(dst, px, py, r, gr, bl, a)
		}
	}
	return true
}

func treeTexture(tree *modtree.ModTree, name string) *image.NRGBA {
	if tree == nil || name == "" {
		return nil
	}
	rel := strings.ReplaceAll(strings.TrimSpace(name), "\\", "/")
	rel = strings.TrimPrefix(rel, "/")
	if !strings.EqualFold(rel[:minInt(len(rel), 5)], "data/") {
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

func sampleTexture(img *image.NRGBA, u, v float64, wrap bool) (float64, float64, float64, float64) {
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
			ix = clampInt(ix, 0, w-1)
			iy = clampInt(iy, 0, h-1)
		}
		weight := (1 - math.Abs(float64(int(tap[0]))-fx)) * (1 - math.Abs(float64(int(tap[1]))-fy))
		o := img.PixOffset(b.Min.X+ix, b.Min.Y+iy)
		a := float64(img.Pix[o+3]) / 255
		acc[0] += weight * float64(img.Pix[o+0]) * a
		acc[1] += weight * float64(img.Pix[o+1]) * a
		acc[2] += weight * float64(img.Pix[o+2]) * a
		acc[3] += weight * float64(img.Pix[o+3])
	}
	if acc[3] <= 0 {
		return 0, 0, 0, 0
	}
	return acc[0] / acc[3], acc[1] / acc[3], acc[2] / acc[3], acc[3] / 255
}

func blendPixel(dst *image.NRGBA, x, y int, r, g, b, a float64) {
	o := dst.PixOffset(x, y)
	dst.Pix[o+0] = clampByte(float32(r*a*255 + float64(dst.Pix[o+0])*(1-a)))
	dst.Pix[o+1] = clampByte(float32(g*a*255 + float64(dst.Pix[o+1])*(1-a)))
	dst.Pix[o+2] = clampByte(float32(b*a*255 + float64(dst.Pix[o+2])*(1-a)))
	dst.Pix[o+3] = 0xFF
}

// --- Текстуры объектов (.rsh / .rtx / .dds / .tga) ---

var materialTextureExtensions = []string{".rsh", ".rtx", ".dds", ".tga"}

// resolveObjectTextures разрешает и декодирует текстуры для всех частей объектов.
func resolveObjectTextures(tree *modtree.ModTree, objects []Object, modelPaths map[string]string) {
	if tree == nil || len(objects) == 0 {
		return
	}
	needed := map[string]bool{}
	for _, obj := range objects {
		for _, p := range obj.Parts {
			if p.Mesh != nil && p.Mesh.Material != "" {
				needed[p.Mesh.Material] = true
			}
		}
	}
	if len(needed) == 0 {
		return
	}
	names := make([]string, 0, len(needed))
	for name := range needed {
		names = append(names, name)
	}
	roots := materialSearchRoots(tree)
	files := resolveMaterials(tree, roots, modelPaths, names)
	decoded := map[string]*Texture{}
	for name, data := range files {
		if tex := decodeMaterialTexture(name, data); tex != nil {
			decoded[name] = tex
		}
	}
	for i := range objects {
		for j := range objects[i].Parts {
			p := &objects[i].Parts[j]
			if p.Mesh != nil && p.Mesh.Material != "" {
				p.Texture = decoded[p.Mesh.Material]
			}
		}
	}
}

func decodeMaterialTexture(key string, data []byte) *Texture {
	if len(data) == 0 {
		return nil
	}
	img, _, err := raster.DecodeRshImage(data, 0)
	if err != nil || img == nil {
		for _, ext := range []string{"rtx", "dds", "tga"} {
			img, _, err = raster.DecodeImageData(data, ext)
			if err == nil && img != nil {
				break
			}
		}
	}
	if err != nil || img == nil {
		return nil
	}
	b := img.Bounds()
	if b.Empty() {
		return nil
	}
	return &Texture{
		Key:    key,
		Width:  b.Dx(),
		Height: b.Dy(),
		Pixels: rgbaPixels(img),
	}
}

func resolveMaterials(tree *modtree.ModTree, roots []string, modelPaths map[string]string, names []string) map[string][]byte {
	files := map[string][]byte{}
	var missing []string
	for _, name := range names {
		modelFile := modelPaths[name]
		data, ok := readMaterial(tree, roots, modelFile, name)
		if !ok {
			missing = append(missing, name)
			continue
		}
		files[name] = data
	}
	for name, file := range findMaterialsInTree(tree, missing) {
		if data, err := tree.ReadTreeFile(file); err == nil {
			files[name] = data
		}
	}
	for name, path := range findMaterialsOnDisk(roots, missing) {
		if _, alreadyFound := files[name]; alreadyFound {
			continue
		}
		if data, err := os.ReadFile(path); err == nil {
			files[name] = data
		}
	}
	return files
}

func readMaterial(tree *modtree.ModTree, roots []string, modelFile, name string) ([]byte, bool) {
	if tree != nil {
		for _, candidate := range materialCandidatePaths(name) {
			if file := tree.FindFile(candidate); file != nil {
				if data, err := tree.ReadTreeFile(file); err == nil {
					return data, true
				}
			}
		}
	}
	for _, root := range roots {
		for _, candidate := range materialCandidatePaths(name) {
			for _, path := range materialDiskPaths(root, candidate) {
				if data, err := os.ReadFile(path); err == nil {
					return data, true
				}
			}
		}
	}
	base := materialBaseName(name)
	base = strings.TrimSuffix(base, filepath.Ext(base))
	if base == "" {
		return nil, false
	}
	modelPath := strings.ReplaceAll(modelFile, "\\", "/")
	if dir := filepath.Dir(modelPath); dir != "" && dir != "." {
		for _, candidate := range materialCandidatePaths(filepath.ToSlash(filepath.Join(dir, base))) {
			if tree != nil {
				if file := tree.FindFile(candidate); file != nil {
					if data, err := tree.ReadTreeFile(file); err == nil {
						return data, true
					}
				}
			}
			for _, root := range roots {
				for _, path := range materialDiskPaths(root, candidate) {
					if data, err := os.ReadFile(path); err == nil {
						return data, true
					}
				}
			}
		}
	}
	if tree != nil {
		for _, ext := range materialTextureExtensions {
			fileName := base + ext
			if file := tree.FindMaterialByName(fileName); file != nil {
				if data, err := tree.ReadTreeFile(file); err == nil {
					return data, true
				}
			}
			if file := findMaterialInTree(tree, fileName); file != nil {
				if data, err := tree.ReadTreeFile(file); err == nil {
					return data, true
				}
			}
		}
	}
	return nil, false
}

func materialCandidatePaths(name string) []string {
	name = strings.TrimSpace(strings.ReplaceAll(name, "\\", "/"))
	if name == "" {
		return nil
	}
	ext := strings.ToLower(filepath.Ext(name))
	if ext != "" {
		return []string{name}
	}
	out := make([]string, 0, len(materialTextureExtensions))
	for _, suffix := range materialTextureExtensions {
		out = append(out, name+suffix)
	}
	return out
}

func materialCandidateNames(name string) []string {
	paths := materialCandidatePaths(name)
	out := make([]string, 0, len(paths))
	for _, candidate := range paths {
		out = append(out, materialBaseName(candidate))
	}
	return out
}

func materialDiskPaths(root, candidate string) []string {
	rel := filepath.FromSlash(strings.ReplaceAll(candidate, "\\", "/"))
	paths := []string{filepath.Join(root, rel)}
	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) > 1 && strings.EqualFold(parts[0], "Data") {
		paths = append(paths, filepath.Join(root, filepath.Join(parts[1:]...)))
	}
	return paths
}

func findMaterialsInTree(tree *modtree.ModTree, names []string) map[string]*modtree.TreeFile {
	if tree == nil || tree.Root == nil || len(names) == 0 {
		return nil
	}
	want := make(map[string]bool, len(names))
	for _, name := range names {
		for _, fileName := range materialCandidateNames(name) {
			want[strings.ToLower(fileName)] = true
		}
	}
	found := findMaterialFilesInTree(tree, want)
	out := make(map[string]*modtree.TreeFile, len(found))
	for _, name := range names {
		for _, candidate := range materialCandidateNames(name) {
			if file := found[strings.ToLower(candidate)]; file != nil {
				out[name] = file
				break
			}
		}
	}
	return out
}

func findMaterialInTree(tree *modtree.ModTree, fileName string) *modtree.TreeFile {
	if tree == nil || tree.Root == nil || fileName == "" {
		return nil
	}
	return findMaterialFilesInTree(tree, map[string]bool{strings.ToLower(fileName): true})[strings.ToLower(fileName)]
}

func findMaterialFilesInTree(tree *modtree.ModTree, want map[string]bool) map[string]*modtree.TreeFile {
	found := make(map[string]*modtree.TreeFile, len(want))
	var walk func(*modtree.TreeFolder, bool)
	walk = func(folder *modtree.TreeFolder, textureShare bool) {
		textureShare = textureShare || strings.EqualFold(folder.Name, "texture_share")
		for _, file := range folder.Files {
			key := strings.ToLower(file.Name)
			if !want[key] || file.Source == nil {
				continue
			}
			old, exists := found[key]
			oldTexture := exists && treePathInFolder(old.FullPath, "texture_share")
			if !exists || (textureShare && !oldTexture) || (textureShare == oldTexture && sourceMoreSpecific(file, old)) {
				found[key] = file
			}
		}
		for _, child := range folder.Children {
			walk(child, textureShare)
		}
	}
	walk(tree.Root, false)
	return found
}

func sourceMoreSpecific(candidate, current *modtree.TreeFile) bool {
	if candidate == nil || candidate.Source == nil {
		return false
	}
	if current == nil || current.Source == nil {
		return true
	}
	return candidate.Source.Priority < current.Source.Priority
}

func treePathInFolder(path, folder string) bool {
	for _, part := range strings.Split(strings.ToLower(strings.ReplaceAll(path, "/", "\\")), `\`) {
		if part == folder {
			return true
		}
	}
	return false
}

func materialSearchRoots(tree *modtree.ModTree) []string {
	if tree == nil {
		return nil
	}
	var roots []string
	seen := map[string]bool{}
	add := func(p string) {
		if p == "" {
			return
		}
		clean := filepath.Clean(p)
		if seen[clean] {
			return
		}
		seen[clean] = true
		roots = append(roots, clean)
	}
	if tree.ModFolder != "" && tree.AppPath != "" {
		add(filepath.Join(tree.AppPath, tree.ModFolder))
	}
	for _, src := range tree.Sources {
		if !src.IsSGA {
			add(src.DiskRoot)
		}
	}
	return roots
}

func findMaterialsOnDisk(roots []string, names []string) map[string]string {
	if len(names) == 0 {
		return nil
	}
	want := map[string][]string{}
	for _, name := range names {
		for _, fileName := range materialCandidateNames(name) {
			key := strings.ToLower(fileName)
			want[key] = append(want[key], name)
		}
	}
	found := map[string]string{}
	for _, root := range roots {
		if len(want) == 0 {
			break
		}
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || len(want) == 0 {
				return nil
			}
			if d.IsDir() {
				return nil
			}
			if names, ok := want[strings.ToLower(d.Name())]; ok {
				for _, name := range names {
					found[name] = path
				}
				delete(want, strings.ToLower(d.Name()))
			}
			return nil
		})
	}
	return found
}

func materialBaseName(name string) string {
	base := name
	if i := strings.LastIndexAny(base, "/\\"); i >= 0 {
		base = base[i+1:]
	}
	return base
}

func clampByte(v float32) byte {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return byte(v + 0.5)
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
