package viewer

import (
	engine "dow-mod-studio/internal/3d_engine"
	"dow-mod-studio/internal/app/render"
	"dow-mod-studio/internal/app/uikit"
	"dow-mod-studio/internal/app/uistate"
	"dow-mod-studio/internal/chunky"
	"dow-mod-studio/internal/fx"
	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/raster"
	"dow-mod-studio/internal/whm"
	"encoding/binary"
	"fmt"
	"github.com/fogleman/fauxgl"
	"image"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ModelMaterialDiagnostic is what the material inspector can tell about a
// material reference, whether resolution and decoding succeeded or not.
type ModelMaterialDiagnostic = uistate.ModelMaterialDiagnostic

// loadModelMaterials resolves every mesh material of a model to its diffuse
// map. A .whm may name a .rsh, a compiled .rtx, or a bare base name; the
// texture is often in a different mod than the model, so shared game art and
// files beside the model are both part of the lookup.
func LoadModelMaterials(ui *uistate.State, v *ModelViewer, result chan<- ModelMaterialResult) {
	if v == nil || v.Model == nil || ui.ModTree == nil {
		return
	}
	v.Loading = true
	needed := map[string]bool{}
	for _, mesh := range v.Model.Meshes {
		if mesh.Material != "" {
			// Keep results under the exact key the renderer uses. The resolver
			// follows MaterialPath via the model's shared-shader channel, but
			// returns the bytes keyed by this mesh material label.
			needed[mesh.Material] = true
		}
	}
	names := make([]string, 0, len(needed))
	for name := range needed {
		names = append(names, name)
	}
	tree := ui.ModTree
	model := v.ModelFile
	roots := MaterialSearchRoots(tree)
	// Vulkan uploads block-compressed RSH mip chains directly. RTX is
	// decoded as a compiled image instead, preserving the GPU preview by
	// uploading it as an in-memory diffuse texture.
	raw := true
	go func() {
		files, paths := ResolveModelMaterialsWithPaths(tree, roots, model, names)
		decoded := DecodeModelMaterials(files, raw)
		AttachMaterialSourcePaths(decoded.Diagnostics, paths)
		for _, name := range names {
			if _, ok := decoded.Diagnostics[name]; !ok {
				decoded.Diagnostics[name] = ModelMaterialDiagnostic{Format: "—", Status: "not found"}
			}
		}
		result <- decoded
	}()
}

// ResolveModelMaterials first checks authored paths, then searches the merged
// virtual tree (including parent-mod and game SGA archives), and finally disk.
func ResolveModelMaterials(tree *modtree.ModTree, roots []string, modelFile string, names []string) map[string][]byte {
	files, _ := ResolveModelMaterialsWithPaths(tree, roots, modelFile, names)
	return files
}

// ResolveModelMaterialsWithPaths resolves each material once and keeps the
// actual source path alongside the bytes, including the archive/container
// boundary for assets read from SGA files.
func ResolveModelMaterialsWithPaths(tree *modtree.ModTree, roots []string, modelFile string, names []string) (map[string][]byte, map[string]string) {
	files := map[string][]byte{}
	paths := map[string]string{}
	var missing []string
	for _, name := range names {
		reference := ModelMaterialReference(tree, modelFile, name)
		data, path, ok := ReadModelMaterialWithPath(tree, roots, modelFile, name)
		if reference != "" && !strings.EqualFold(reference, name) {
			// The display/material label is not necessarily a real path or
			// filename (for example, "necron warrior"). Try the shared
			// shader's diffuse reference first, then retain the label as the
			// returned key so the draw can find it.
			data, path, ok = ReadModelMaterialWithPath(tree, roots, modelFile, reference)
		}
		if !ok {
			missing = append(missing, name)
			continue
		}
		files[name], paths[name] = data, path
	}
	for name, file := range FindMaterialsInTree(tree, missing) {
		if data, err := tree.ReadTreeFile(file); err == nil {
			files[name], paths[name] = data, MaterialTreePath(file)
		}
	}
	for name, path := range FindMaterialsOnDisk(roots, missing) {
		if _, alreadyFound := files[name]; alreadyFound {
			continue
		}
		if data, err := os.ReadFile(path); err == nil {
			files[name], paths[name] = data, path
		}
	}
	return files, paths
}

func ModelMaterialReference(tree *modtree.ModTree, modelFile, material string) string {
	if tree == nil || modelFile == "" {
		return ""
	}
	data, err := tree.ReadFile(modelFile)
	if err != nil {
		return ""
	}
	model, err := whm.Parse(data)
	if err != nil {
		return ""
	}
	key := strings.ToLower(strings.TrimSpace(material))
	if reference := model.MaterialPaths[key]; reference != "" {
		return reference
	}
	// Some WHMs only give parseMeshes a positional FOLDSTXT fallback when
	// there is no shader-name mapping. Preserve that material-to-path link
	// too, so display labels still resolve to the canonical diffuse.
	for _, mesh := range model.Meshes {
		if strings.EqualFold(strings.TrimSpace(mesh.Material), strings.TrimSpace(material)) && mesh.MaterialPath != "" {
			return mesh.MaterialPath
		}
	}
	return ""
}

// AttachMaterialSourcePaths pairs decoder results with resolved source paths
// without decoding large files a second time.
func AttachMaterialSourcePaths(diagnostics map[string]ModelMaterialDiagnostic, paths map[string]string) {
	for name, diag := range diagnostics {
		diag.ResolvedPath = paths[name]
		diagnostics[name] = diag
	}
}

func RshMaterialFormat(format int32) string {
	switch format {
	case 8:
		return "RSH / DXT1 (BC1)"
	case 10:
		return "RSH / DXT3 (BC2)"
	case 11:
		return "RSH / DXT5 (BC3)"
	default:
		return fmt.Sprintf("RSH / format %d", format)
	}
}

func DecodedMaterialFormat(format string, data []byte) string {
	format = strings.ToLower(format)
	if format != "rtx" {
		return strings.ToUpper(format)
	}
	chunks, _ := chunky.Parse(data[uikit.MinInt(chunky.PreambleSize, len(data)):], 0)
	var find func([]*chunky.Chunk) string
	find = func(chunks []*chunky.Chunk) string {
		for _, chunk := range chunks {
			if chunk.TypeID == "FOLDIMAG" {
				attr := chunk.Child("DATAATTR")
				if attr != nil && len(attr.Payload) >= 4 {
					switch int(binary.LittleEndian.Uint32(attr.Payload[:4])) {
					case 8:
						return "RTX / DXT1 (BC1)"
					case 10:
						return "RTX / DXT3 (BC2)"
					case 11:
						return "RTX / DXT5 (BC3)"
					case 0, 2:
						return "RTX / TGA"
					}
				}
			}
			if nested := find(chunk.Children); nested != "" {
				return nested
			}
		}
		return "RTX"
	}
	return find(chunks)
}

// DecodeModelMaterials retains compressed bytes for Vulkan and decoded images
// for the software renderer used by diagnostic/fallback paths.
func DecodeModelMaterials(files map[string][]byte, raw bool) ModelMaterialResult {
	res := ModelMaterialResult{
		Materials:   map[string]render.WhmMaterial{},
		Rsh:         map[string][]byte{},
		Diagnostics: map[string]ModelMaterialDiagnostic{},
	}
	for name, data := range files {
		diag := ModelMaterialDiagnostic{Status: "unrecognized image data"}
		rshInfo, _, rshErr := raster.RshRawImage(data, 0)
		var img image.Image
		var err error
		if rshErr == nil {
			diag.Format = RshMaterialFormat(rshInfo.Format)
			if raw {
				res.Rsh[name] = data
			}
			img, _, err = raster.DecodeRshImage(data, 0)
		} else {
			// Texture references in WHM files often omit an extension. The
			// resolved file may therefore be RTX even though the reference key
			// itself has none; detect supported compiled formats by decoding.
			for _, ext := range []string{"rtx", "dds", "tga"} {
				img, _, err = raster.DecodeImageData(data, ext)
				if err == nil && img != nil {
					diag.Format = DecodedMaterialFormat(ext, data)
					break
				}
			}
		}
		if err == nil && img != nil {
			diag.Status = "decoded"
			res.Materials[name] = render.WhmMaterial{Texture: render.NewModelTexture(img), Color: render.RgbaWhite}
		} else if rshErr != nil {
			diag.Status = "decode failed"
		}
		res.Diagnostics[name] = diag
	}
	for name := range files {
		if _, ok := res.Materials[name]; !ok {
			res.Missing++
		}
	}
	return res
}

// MaterialTextureExtensions are tried in the game's effective order: the
// shared material format first, then compiled textures (which some models name
// implicitly) and the source-friendly interchange formats.
var MaterialTextureExtensions = []string{".rsh", ".rtx", ".dds", ".tga"}

func MaterialCandidatePaths(name string) []string {
	name = strings.TrimSpace(strings.ReplaceAll(name, "\\", "/"))
	if name == "" {
		return nil
	}
	ext := strings.ToLower(filepath.Ext(name))
	if ext != "" {
		return []string{name}
	}
	out := make([]string, 0, len(MaterialTextureExtensions))
	for _, suffix := range MaterialTextureExtensions {
		out = append(out, name+suffix)
	}
	return out
}

func MaterialCandidateNames(name string) []string {
	paths := MaterialCandidatePaths(name)
	out := make([]string, 0, len(paths))
	for _, candidate := range paths {
		out = append(out, fx.MaterialBaseName(candidate))
	}
	return out
}

// MaterialDiskPaths returns both useful interpretations of a virtual path:
// roots may be the mod root (which needs the Data prefix) or the Data folder
// itself (which does not).
func MaterialDiskPaths(root, candidate string) []string {
	rel := filepath.FromSlash(strings.ReplaceAll(candidate, "\\", "/"))
	paths := []string{filepath.Join(root, rel)}
	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) > 1 && strings.EqualFold(parts[0], "Data") {
		paths = append(paths, filepath.Join(root, filepath.Join(parts[1:]...)))
	}
	return paths
}

// FindMaterialsInTree returns the best merged-tree file for each requested
// material base name. Current-mod files win within texture_share; texture_share
// files win over same-named files elsewhere.
func FindMaterialsInTree(tree *modtree.ModTree, names []string) map[string]*modtree.TreeFile {
	if tree == nil || tree.Root == nil || len(names) == 0 {
		return nil
	}
	want := make(map[string]bool, len(names))
	for _, name := range names {
		for _, fileName := range MaterialCandidateNames(name) {
			want[strings.ToLower(fileName)] = true
		}
	}
	found := FindMaterialFilesInTree(tree, want)
	out := make(map[string]*modtree.TreeFile, len(found))
	for _, name := range names {
		for _, candidate := range MaterialCandidateNames(name) {
			if file := found[strings.ToLower(candidate)]; file != nil {
				out[name] = file
				break
			}
		}
	}
	return out
}

func FindMaterialInTree(tree *modtree.ModTree, fileName string) *modtree.TreeFile {
	if tree == nil || tree.Root == nil || fileName == "" {
		return nil
	}
	return FindMaterialFilesInTree(tree, map[string]bool{strings.ToLower(fileName): true})[strings.ToLower(fileName)]
}

func FindMaterialFilesInTree(tree *modtree.ModTree, want map[string]bool) map[string]*modtree.TreeFile {
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
			oldTexture := exists && TreePathInFolder(old.FullPath, "texture_share")
			if !exists || (textureShare && !oldTexture) || (textureShare == oldTexture && SourceMoreSpecific(file, old)) {
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

func SourceMoreSpecific(candidate, current *modtree.TreeFile) bool {
	if candidate == nil || candidate.Source == nil {
		return false
	}
	if current == nil || current.Source == nil {
		return true
	}
	return candidate.Source.Priority < current.Source.Priority
}

func TreePathInFolder(path, folder string) bool {
	for _, part := range strings.Split(strings.ToLower(strings.ReplaceAll(path, "/", "\\")), `\`) {
		if part == folder {
			return true
		}
	}
	return false
}

// MaterialSearchRoots lists on-disk data folders, most specific first.
func MaterialSearchRoots(tree *modtree.ModTree) []string {
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

// ReadModelMaterial resolves one .whm material reference to image bytes.
func ReadModelMaterial(tree *modtree.ModTree, roots []string, modelFile, name string) ([]byte, bool) {
	data, _, ok := ReadModelMaterialWithPath(tree, roots, modelFile, name)
	return data, ok
}

func ReadModelMaterialWithPath(tree *modtree.ModTree, roots []string, modelFile, name string) ([]byte, string, bool) {
	if tree != nil {
		for _, candidate := range MaterialCandidatePaths(name) {
			if file := tree.FindFile(candidate); file != nil {
				if data, err := tree.ReadTreeFile(file); err == nil {
					return data, MaterialTreePath(file), true
				}
			}
		}
	}
	for _, root := range roots {
		for _, candidate := range MaterialCandidatePaths(name) {
			for _, path := range MaterialDiskPaths(root, candidate) {
				if data, err := os.ReadFile(path); err == nil {
					return data, path, true
				}
			}
		}
	}
	base := fx.MaterialBaseName(name)
	base = strings.TrimSuffix(base, filepath.Ext(base))
	if base == "" {
		return nil, "", false
	}
	modelPath := strings.ReplaceAll(modelFile, "\\", "/")
	if dir := filepath.Dir(modelPath); dir != "" && dir != "." {
		for _, candidate := range MaterialCandidatePaths(filepath.ToSlash(filepath.Join(dir, base))) {
			if tree != nil {
				if file := tree.FindFile(candidate); file != nil {
					if data, err := tree.ReadTreeFile(file); err == nil {
						return data, MaterialTreePath(file), true
					}
				}
			}
			for _, root := range roots {
				for _, path := range MaterialDiskPaths(root, candidate) {
					if data, err := os.ReadFile(path); err == nil {
						return data, path, true
					}
				}
			}
		}
	}
	if tree != nil {
		for _, ext := range MaterialTextureExtensions {
			fileName := base + ext
			if file := tree.FindMaterialByName(fileName); file != nil {
				if data, err := tree.ReadTreeFile(file); err == nil {
					return data, MaterialTreePath(file), true
				}
			}
			if file := FindMaterialInTree(tree, fileName); file != nil {
				if data, err := tree.ReadTreeFile(file); err == nil {
					return data, MaterialTreePath(file), true
				}
			}
		}
	}
	return nil, "", false
}

func MaterialTreePath(file *modtree.TreeFile) string {
	if file == nil {
		return ""
	}
	if file.Source != nil && file.Source.IsSGA {
		return file.Source.ArchivePath + " :: " + file.FullPath
	}
	if file.DiskPath != "" {
		return file.DiskPath
	}
	return file.FullPath
}

// FindMaterialsOnDisk answers remaining references with one walk per root.
func FindMaterialsOnDisk(roots []string, names []string) map[string]string {
	if len(names) == 0 {
		return nil
	}
	want := map[string][]string{}
	for _, name := range names {
		for _, fileName := range MaterialCandidateNames(name) {
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
				return nil //nolint:nilerr // unreadable folders contain no result
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

// UvNet is the texture mapping as two linear expressions: each output
// coordinate is a combination of the two input coordinates plus the offset.
// The coefficients are the mapping after the mirror, the scale and the turn
// have been multiplied together, which is the only form worth printing.
type UvNet struct {
	u   [2]float32 // coefficients of (u, v) for the new u
	v   [2]float32 // coefficients of (u, v) for the new v
	off [2]float32
}

// uvMaterials is the model's own list of textures, in the order its sub meshes
// name them and without repeats, with the shared mapping at the front under an
// empty name. The empty one is first because it is what a model with a single
// map, or a reader who has not yet found out that it has two, wants to be
// editing.
func UvSelectionList(v *ModelViewer) []string {
	out := []string{""}
	if v == nil || v.Model == nil {
		return out
	}
	seen := make(map[string]bool, len(v.Model.Meshes))
	for i := range v.Model.Meshes {
		m := v.Model.Meshes[i].Material
		if m == "" || seen[m] {
			continue
		}
		seen[m] = true
		out = append(out, m)
	}
	return out
}

// uvCurrent is the mapping the controls are editing right now: the selected
// material's own, or the one they all share. The pointer is what lets a
// button change the mapping without every reader of it having to know which
// one is selected.
func UvCurrent(v *ModelViewer) *render.UVMapping {
	if v == nil {
		return nil
	}
	if v.UvSelection == "" {
		return &v.Uv
	}
	if v.UvMaterials == nil {
		v.UvMaterials = make(map[string]*render.UVMapping)
	}
	m, ok := v.UvMaterials[v.UvSelection]
	if !ok {
		// A material picked for the first time starts from the shared
		// mapping, which is almost always what it wants: the shared
		// mapping is what every other texture is using.
		shared := v.Uv
		m = &shared
		v.UvMaterials[v.UvSelection] = m
	}
	return m
}

// selectUVMapping chooses which material the controls edit. The sliders are
// refilled from the new choice the next time the panel is laid out, not here,
// so that a button press does not move the numbers under a pointer that is
// still over the old ones.
func SelectUVMapping(v *ModelViewer, material string) {
	if v == nil || v.UvSelection == material {
		return
	}
	UvPull(v)
	v.UvSelection = material
	v.UvSelectionDirty = true
	Invalidate(v)
}

// uvOffset reads the two offset sliders as the shift of the map over the model,
// in units of the map itself.
func UvOffset(v *ModelViewer) [2]float32 {
	if v == nil {
		return [2]float32{}
	}
	return [2]float32{
		ModelUVMinOffset + v.UvU.Value*(ModelUVMaxOffset-ModelUVMinOffset),
		ModelUVMinOffset + v.UvV.Value*(ModelUVMaxOffset-ModelUVMinOffset),
	}
}

// uvScaleValue reads the scale slider. One is the middle of its travel and is
// what an untouched model is drawn with.
func UvScaleValue(v *ModelViewer) float32 {
	if v == nil {
		return 1
	}
	return ModelUVMinScale + v.UvScale.Value*(ModelUVMaxScale-ModelUVMinScale)
}

// uvPush fills the sliders from the mapping they are editing. It runs when the
// selection changes, and once at the start, so that the controls always show
// the numbers of the mapping the frame is actually being drawn with.
func UvPush(v *ModelViewer) {
	m := UvCurrent(v)
	if m == nil {
		return
	}
	v.UvSelectionDirty = false
	scale := m.Scale
	if scale <= 0 {
		scale = 1
	}
	v.UvU.Value = float32(engine.ClampFloat64(float64((m.Offset[0]-ModelUVMinOffset)/float32(ModelUVMaxOffset-ModelUVMinOffset)), 0, 1))
	v.UvV.Value = float32(engine.ClampFloat64(float64((m.Offset[1]-ModelUVMinOffset)/float32(ModelUVMaxOffset-ModelUVMinOffset)), 0, 1))
	v.UvScale.Value = float32(engine.ClampFloat64(float64((scale-ModelUVMinScale)/(ModelUVMaxScale-ModelUVMinScale)), 0, 1))
}

// uvPull writes the sliders back into the mapping they are editing. It runs
// after the sliders are laid out, because that is when a drag has been read.
func UvPull(v *ModelViewer) {
	m := UvCurrent(v)
	if m == nil {
		return
	}
	m.Offset = UvOffset(v)
	m.Scale = UvScaleValue(v)
}

// resetUVMapping puts the selected texture's map back where the file has it. It
// is its own button because a reader who has been dragging the sliders needs
// one, and because "Reset view" only moves the camera. With one texture
// selected out of several it is that one and not the model that goes back.
func ResetUVMapping(v *ModelViewer) {
	if v == nil {
		return
	}
	if m := UvCurrent(v); m != nil {
		*m = render.UVIdentityMapping()
	}
	v.UvSelectionDirty = true
	Invalidate(v)
}

// UvMappingNet multiplies the four operations together into UvNet.
//
// Multiplying them out is not tidiness, it is the whole point. The operations
// do not commute and they do not all cancel in the same places: mirroring U
// and then turning a half turn negates U twice over, which leaves U exactly as
// the file had it and flips V instead. A panel that printed the operations
// would say the model is mirrored when it is not, and a reader writing those
// numbers into a model would get something that looks almost right.
func UvMappingNet(mirror bool, turns int, scale float32, off [2]float32) UvNet {
	a, b, c, d := float32(1), float32(0), float32(0), float32(1)
	if mirror {
		a = -a
	}
	a, b, c, d = a*scale, b*scale, c*scale, d*scale
	switch ((turns % 4) + 4) % 4 {
	case 1: // (u, v) → (v, −u)
		a, b, c, d = c, d, -a, -b
	case 2: // (u, v) → (−u, −v)
		a, b, c, d = -a, -b, -c, -d
	case 3: // (u, v) → (−v, u)
		a, b, c, d = -c, -d, a, b
	}
	return UvNet{u: [2]float32{a, b}, v: [2]float32{c, d}, off: off}
}

// net is the viewer's own mapping in that form.
func Net(v *ModelViewer) UvNet {
	m := render.UVMapping{}
	if cur := UvCurrent(v); cur != nil {
		m = *cur
	}
	return UvMappingNet(m.Mirror, m.Rotate, m.Scale, m.Offset)
}

// uvMappingSummary writes the mapping as the two expressions it works out to,
// which is the form it can be written back down in: a line of this program's
// own code, or the pair of numbers a model would carry in two more UV channels.
func UvMappingSummary(v *ModelViewer) string {
	n := Net(v)
	return UvNetTerm(n.u, n.off[0]) + ", " + UvNetTerm(n.v, n.off[1])
}

// UvNetTerm is one of those two expressions: whichever of u and v the mapping
// actually uses, multiplied, and then the shift added to it.
func UvNetTerm(coef [2]float32, off float32) string {
	var parts []string
	for i, axis := range [2]string{"u", "v"} {
		switch c := coef[i]; {
		case c > 0:
			parts = append(parts, axis+"×"+ModelUVFixed(c, 3))
		case c < 0:
			parts = append(parts, "-"+axis+"×"+ModelUVFixed(-c, 3))
		}
	}
	if len(parts) == 0 {
		parts = append(parts, "0")
	}
	s := strings.Join(parts, "+")
	if off != 0 {
		s += ModelUVAdd(off)
	}
	return s
}

// uvMappingSequence is the order the mapping is applied in, which is how the
// line above can be checked by hand. It is kept because the two disagree often
// enough to be worth showing: a mirror and a half turn cancel on one axis and
// swap to the other, and seeing the steps beside the result is what makes that
// obvious rather than surprising.
func UvMappingSequence(v *ModelViewer) string {
	m := render.UVMapping{}
	if cur := UvCurrent(v); cur != nil {
		m = *cur
	}
	var parts []string
	if m.Mirror {
		parts = append(parts, "mirror U")
	}
	scale := m.Scale
	if scale <= 0 {
		scale = 1
	}
	parts = append(parts, "×"+ModelUVFixed(scale, 3))
	if turns := ((m.Rotate % 4) + 4) % 4; turns != 0 {
		parts = append(parts, "turn "+uikit.ItoaPlain(turns*90)+"°")
	}
	off := m.Offset
	if off[0] != 0 || off[1] != 0 {
		parts = append(parts, "shift "+ModelUVFixed(off[0], 3)+", "+ModelUVFixed(off[1], 3))
	}
	return strings.Join(parts, " → ")
}

// uvMappingNote is the one thing about a mapping that is worth saying in words,
// or nothing. An offset of a whole number of map tiles is the same as no
// offset, because the map repeats exactly that often: a reader who has just
// dragged one to the far end of its travel and seen nothing happen is looking
// at a bug in the model, not at their own mistake.
func UvMappingNote(v *ModelViewer) string {
	off := [2]float32{}
	if cur := UvCurrent(v); cur != nil {
		off = cur.Offset
	}
	if UvWholeTiles(off[0]) && UvWholeTiles(off[1]) {
		return "A whole number of tiles either way: the same as no offset."
	}
	if UvWholeTiles(off[0]) {
		return "U is a whole number of tiles, so only V is moving the map."
	}
	if UvWholeTiles(off[1]) {
		return "V is a whole number of tiles, so only U is moving the map."
	}
	return ""
}

// uvUnmappedMeshes names the sub meshes that have no map of their own, which
// is the commonest reason a model's texture looks wrong and has nothing to do
// with the UVs at all: such a mesh is drawn from a one by one white map, so it
// comes out as an untextured part of an otherwise textured model, and moving
// the sliders will not change it.
func UvUnmappedMeshes(v *ModelViewer) string {
	if v == nil || v.Model == nil || len(v.Materials) == 0 {
		return ""
	}
	var missing []string
	for i := range v.Model.Meshes {
		m := v.Model.Meshes[i]
		if m.Material == "" {
			continue
		}
		if _, ok := v.Materials[m.Material]; !ok {
			missing = append(missing, m.Name)
		}
	}
	if len(missing) == 0 {
		return ""
	}
	const show = 3
	names := missing
	if len(names) > show {
		names = names[:show]
	}
	out := uikit.ItoaPlain(len(missing)) + " of " + uikit.ItoaPlain(len(v.Model.Meshes)) + " meshes: "
	for i, n := range names {
		if i > 0 {
			out += ", "
		}
		out += n
	}
	if len(missing) > show {
		out += " and " + uikit.ItoaPlain(len(missing)-show) + " more"
	}
	return out
}

// ModelUVFixed is a decimal with a fixed number of places, always with a point
// and an ASCII minus, so that a value copied out of the panel is a value that
// can be pasted into a model or a line of code without being tidied up first.
func ModelUVFixed(v float32, places int) string {
	neg := v < 0
	if neg {
		v = -v
	}
	scale := 1
	for i := 0; i < places; i++ {
		scale *= 10
	}
	n := int(float64(v)*float64(scale) + 0.5)
	s := uikit.ItoaPlain(n/scale) + "." + PadZeroes(n%scale, places)
	if neg {
		s = "-" + s
	}
	return s
}

// PadZeroes writes a small number with leading zeroes, so that a value reads
// the same width whichever it happens to be.
func PadZeroes(n, places int) string {
	out := make([]byte, places)
	for i := places - 1; i >= 0; i-- {
		out[i] = byte('0' + n%10)
		n /= 10
	}
	return string(out)
}

// ModelUVAdd is an offset written as it would be added, with the sign that
// makes it an addition either way, so that the summary reads as arithmetic
// rather than as a list of magnitudes.
func ModelUVAdd(v float32) string {
	if v < 0 {
		return "-" + ModelUVFixed(-v, 3)
	}
	return "+" + ModelUVFixed(v, 3)
}

// boneTrack returns the animation track that belongs to the picked bone, or nil
// when the picked bone has no track in the current clip.
func BoneTrack(v *ModelViewer) *whm.Track {
	if v.Anim == nil || v.BoneIndex < 0 || v.BoneIndex >= len(v.Model.Bones) {
		return nil
	}
	for i := range v.Anim.Bones {
		if v.Anim.Bones[i].Bone == v.BoneIndex {
			return &v.Anim.Bones[i]
		}
	}
	return nil
}

// boneKeyFrames is the set of frames on which the picked bone has a key, or nil
// when there is nothing to snap to.
func BoneKeyFrames(v *ModelViewer) []int {
	t := BoneTrack(v)
	if t == nil || v.Anim.NumFrames < 1 {
		return nil
	}
	seen := make(map[int]bool)
	add := func(t float32) {
		seen[FrameAt(float64(t), v.Anim.NumFrames)] = true
	}
	for i := range t.PosKey {
		add(t.PosKey[i].Time)
	}
	for i := range t.RotKey {
		add(t.RotKey[i].Time)
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]int, 0, len(seen))
	for f := range seen {
		out = append(out, f)
	}
	sort.Ints(out)
	return out
}

// boneKeyCount is how many keyframes the picked bone has in the current clip,
// counting position and rotation keys together.
func BoneKeyCount(v *ModelViewer) int {
	t := BoneTrack(v)
	if t == nil {
		return 0
	}
	return len(t.PosKey) + len(t.RotKey)
}

// boneKeySelect picks the bone keyframe closest to the current playhead, and
// seeks so that frame is on show. When there is no key near the playhead the
// picked key is cleared instead.
func BoneKeySelect(v *ModelViewer) {
	if v.BoneIndex < 0 {
		v.BoneKeyIndex = -1
		v.BoneKeyMode = -1
		return
	}
	frame, _ := CurrentFrame(v)
	if idx, mode, ok := BoneKeyFrame(v, frame); ok {
		v.BoneKeyIndex = idx
		v.BoneKeyMode = mode
		if f, ok := BoneKeyFrameAt(v); ok {
			SeekFrame(v, f)
		}
	} else {
		v.BoneKeyIndex = -1
		v.BoneKeyMode = -1
	}
}

// setBoneKeyPositionFromPose writes the picked bone's pose position at the
// current playhead into the picked position key, which is what the "Set pos
// from pose" button in the bone parameters panel does.
func SetBoneKeyPositionFromPose(v *ModelViewer) {
	if v.BoneKeyMode != 0 || v.BoneKeyIndex < 0 || v.BoneIndex < 0 || v.Anim == nil {
		return
	}
	t := BoneTrack(v)
	if t == nil || v.BoneKeyIndex >= len(t.PosKey) {
		return
	}
	bone := v.Model.Bones[v.BoneIndex]
	pose := Pose(v)
	if v.BoneIndex >= len(pose.Bones) {
		return
	}
	worldBone := pose.Bones[v.BoneIndex]
	var worldParent fauxgl.Matrix
	if bone.Parent >= 0 && bone.Parent < len(pose.Bones) {
		worldParent = pose.Bones[bone.Parent]
	} else {
		worldParent = whm.RootFrame
	}
	local := worldParent.Inverse().MulPosition(BoneAtPose(worldBone))
	t.PosKey[v.BoneKeyIndex].V = [3]float32{float32(local.X), float32(local.Y), float32(local.Z)}
	Invalidate(v)
}

// setBoneKeyRotationFromPose writes the picked bone's pose rotation at the
// current playhead into the picked rotation key, which is what the "Set rot
// from pose" button in the bone parameters panel does.
func SetBoneKeyRotationFromPose(v *ModelViewer) {
	if v.BoneKeyMode != 1 || v.BoneKeyIndex < 0 || v.BoneIndex < 0 || v.Anim == nil {
		return
	}
	track := BoneTrack(v)
	if track == nil || v.BoneKeyIndex >= len(track.RotKey) {
		return
	}
	bone := v.Model.Bones[v.BoneIndex]
	pose := Pose(v)
	if v.BoneIndex >= len(pose.Bones) {
		return
	}
	worldBone := pose.Bones[v.BoneIndex]
	var worldParent fauxgl.Matrix
	if bone.Parent >= 0 && bone.Parent < len(pose.Bones) {
		worldParent = pose.Bones[bone.Parent]
	} else {
		worldParent = whm.RootFrame
	}
	localMat := worldParent.Inverse().Mul(worldBone)
	track.RotKey[v.BoneKeyIndex].V = MatrixQuat(localMat)
	Invalidate(v)
}

// MatrixQuat extracts a quaternion (x, y, z, w) from the rotation part of a
// 4x4 matrix. It is the inverse of render.QuatMatrix, which builds the matrix from
// that same [4]float32 layout, so a "Set rot from pose" write round-trips
// through the pose unchanged.
//
// fauxgl ships no quaternion type, so the standard Shepperd branches are
// written out against the matrix's named fields.
func MatrixQuat(m fauxgl.Matrix) [4]float32 {
	tr := m.X00 + m.X11 + m.X22
	var q [4]float32
	if tr > 0 {
		s := math.Sqrt(tr+1.0) * 2.0
		q[3] = float32(0.25 * s)
		q[0] = float32((m.X21 - m.X12) / s)
		q[1] = float32((m.X02 - m.X20) / s)
		q[2] = float32((m.X10 - m.X01) / s)
		return q
	}
	if m.X00 > m.X11 && m.X00 > m.X22 {
		s := math.Sqrt(1.0+m.X00-m.X11-m.X22) * 2.0
		q[0] = float32(0.25 * s)
		q[1] = float32((m.X01 + m.X10) / s)
		q[2] = float32((m.X02 + m.X20) / s)
		q[3] = float32((m.X21 - m.X12) / s)
		return q
	}
	if m.X11 > m.X22 {
		s := math.Sqrt(1.0+m.X11-m.X00-m.X22) * 2.0
		q[0] = float32((m.X01 + m.X10) / s)
		q[1] = float32(0.25 * s)
		q[2] = float32((m.X12 + m.X21) / s)
		q[3] = float32((m.X02 - m.X20) / s)
		return q
	}
	s := math.Sqrt(1.0+m.X22-m.X00-m.X11) * 2.0
	q[0] = float32((m.X02 + m.X20) / s)
	q[1] = float32((m.X12 + m.X21) / s)
	q[2] = float32(0.25 * s)
	q[3] = float32((m.X10 - m.X01) / s)
	return q
}

// BoneAtPose returns the world-space origin of a bone's pose matrix, which is
// the bone's position under the pose on show. MulPosition on the zero vector
// reads the translation column, the same idiom model3d_overlay.go uses for
// bone origins.
func BoneAtPose(worldBone fauxgl.Matrix) fauxgl.Vector {
	return worldBone.MulPosition(fauxgl.Vector{})
}

// boneKeyFrame picks the bone keyframe index and mode closest to the given frame.
func BoneKeyFrame(v *ModelViewer, frame int) (int, int, bool) {
	t := BoneTrack(v)
	if t == nil {
		return -1, -1, false
	}
	bestIdx := 0
	bestMode := 0
	bestDist := int(1e9)
	for i := range t.PosKey {
		f := FrameAt(float64(t.PosKey[i].Time), v.Anim.NumFrames)
		d := engine.AbsInt(f - frame)
		if d < bestDist {
			bestDist = d
			bestIdx = i
			bestMode = 0
		}
	}
	for i := range t.RotKey {
		f := FrameAt(float64(t.RotKey[i].Time), v.Anim.NumFrames)
		d := engine.AbsInt(f - frame)
		if d < bestDist {
			bestDist = d
			bestIdx = i
			bestMode = 1
		}
	}
	if bestDist > engine.AbsInt(v.Anim.NumFrames) {
		return -1, -1, false
	}
	return bestIdx, bestMode, true
}

// boneKeyFrameAt returns the frame a bone keyframe sits on, and whether it has
// one to sit on. This is the frame the scrub bar can snap to for this bone key.
func BoneKeyFrameAt(v *ModelViewer) (int, bool) {
	if v.BoneKeyIndex < 0 || v.BoneKeyMode < 0 || v.Anim == nil || BoneTrack(v) == nil {
		return 0, false
	}
	track := BoneTrack(v)
	var keyTime float32
	switch v.BoneKeyMode {
	case 0:
		if v.BoneKeyIndex < len(track.PosKey) {
			keyTime = track.PosKey[v.BoneKeyIndex].Time
		}
	case 1:
		if v.BoneKeyIndex < len(track.RotKey) {
			keyTime = track.RotKey[v.BoneKeyIndex].Time
		}
	}
	if keyTime <= 0 {
		return 0, false
	}
	return FrameAt(float64(keyTime), v.Anim.NumFrames), true
}

// pose evaluates the current animation time, or the bind pose when stopped.
//
// The result is kept until the playhead moves. One frame asks for it three or
// four times over - the effects need it, the marker overlay needs it, the
// renderer needs it - and each ask walks the whole skeleton, inverts every
// bone and writes two matrices per bone into fresh slices. During playback
// that is the largest thing the preview allocates, and it is the same answer
// every time.
func Pose(v *ModelViewer) uistate.WhmPose {
	if v == nil || v.Model == nil {
		return uistate.WhmPose{}
	}
	if len(v.Anims) > 0 {
		if v.PoseValid && v.PoseAnim == v.Anim && v.PoseTime == v.Time {
			return v.PoseCache
		}
		times := make([]float32, len(v.Anims))
		for i, anim := range v.Anims {
			duration := AnimClipSeconds(anim)
			if duration > 0 && v.AnimPlaybackDuration > 0 {
				clipTime := math.Mod(float64(v.Time)*v.AnimPlaybackDuration, duration) / duration
				if !v.Playing && v.AnimPlaybackDuration > duration {
					clipTime = float64(v.Time)
				}
				times[i] = float32(clipTime)
			}
		}
		p := v.Model.PoseAtAnimationTimes(v.Anims, times)
		v.PoseCache, v.PoseAnim, v.PoseTime, v.PoseValid = p, v.Anim, v.Time, true
		return p
	}
	if v.Anim == nil {
		return v.Model.RestPose()
	}
	if v.PoseValid && v.PoseAnim == v.Anim && v.PoseTime == v.Time {
		return v.PoseCache
	}
	p := v.Model.PoseAt(v.Anim, v.Time)
	v.PoseCache, v.PoseAnim, v.PoseTime, v.PoseValid = p, v.Anim, v.Time, true
	return p
}

// stepFrame moves the play head one frame of the clip forward or back, which
// is how a single pose of an animation is looked at. The model is held still
// while it is being wound: a step that the clock immediately moved on from
// would be a step that does nothing.
//
// The play head is kept inside the clip rather than wrapped, so that holding
// the button down walks to the first frame and stays there, which is what
// winding a tape by hand does. The last frame is the same as the first, so
// stepping past it lands on the start rather than on a frame that is not there.
func StepFrame(v *ModelViewer, delta int) {
	if v == nil || v.Model == nil || v.Anim == nil || delta == 0 {
		return
	}
	frames := v.Anim.NumFrames
	if frames <= 0 {
		return
	}
	v.Playing = false
	// The clock is zeroed so that pressing play afterwards starts from the
	// frame that was wound to rather than jumping a whole second ahead of it.
	v.LastFrame = time.Time{}
	step := float64(delta) / float64(frames)
	// The pose is sampled at the middle of a frame, so a play head of exactly
	// one is the last frame, not the one after it.
	t := float64(v.Time) + step
	t = math.Mod(t, 1)
	if t < 0 {
		t++
	}
	v.Time = float32(t)
	if float64(v.Time) >= 1 {
		v.Time = 0
	}
	Invalidate(v)
}

// FrameReadTolerance is how close to a frame boundary the playhead has to be
// for the readout to name that frame, measured in frames. See currentFrame.
const FrameReadTolerance = 1e-3

// currentFrame is which frame of the clip is on show, and how many there are.
// A clip with no frame count reports none, since there is nothing to count. The
// play head is rounded rather than cut, so the frame a step lands on and the
// frame the readout names are the same one.
func CurrentFrame(v *ModelViewer) (int, int) {
	if v == nil || v.Anim == nil || v.Anim.NumFrames <= 0 {
		return 0, 0
	}
	// The playhead is a float32 and the frame count an int, so their product
	// lands a hair under the boundary it was aimed at often enough to matter:
	// one step forward on a 301 frame clip computes 301*float32(1/301) as
	// 0.9999999762, which floors back to frame zero and leaves the readout
	// saying "frame 1" while the clip sits on the second. The tolerance is a
	// thousandth of a frame — far finer than anything a scrub can land on —
	// and it only ever moves a reading that is already at the boundary.
	frame := int(math.Floor(float64(v.Anim.NumFrames)*float64(v.Time) + FrameReadTolerance))
	return uikit.MinInt(uikit.MaxInt(frame, 0), v.Anim.NumFrames-1), v.Anim.NumFrames
}

// selectAnim switches the playing clip, wrapping around the list.
func SelectAnim(v *ModelViewer, index int) {
	if v == nil {
		return
	}
	v.Anims = nil
	v.AnimPlaybackDuration = 0
	if v.Model == nil || len(v.Model.Animations) == 0 {
		v.Anim = nil
		v.AnimIndex = 0
		Invalidate(v)
		return
	}
	n := len(v.Model.Animations)
	if index < 0 {
		index = n - 1
	}
	if index >= n {
		index = 0
	}
	v.AnimIndex = index
	v.Anim = &v.Model.Animations[index]
	v.Time = 0
	v.PoseValid = false
	v.LastFrame = time.Time{}
	Invalidate(v)
}

// selectAnimations plays clips belonging to one action together on one model.
// It uses the longest selected clip as the shared playback clock; individual
// clip times are sampled against their own durations.
func SelectAnimations(v *ModelViewer, anims []*uistate.WhmAnimation, playback bool) {
	if v == nil {
		return
	}
	v.Anims = nil
	v.Anims = make([]*uistate.WhmAnimation, 0, len(anims))
	for _, anim := range anims {
		if anim != nil {
			v.Anims = append(v.Anims, anim)
		}
	}
	if len(v.Anims) == 0 {
		v.Anim = nil
		v.AnimPlaybackDuration = 0
		v.PoseValid = false
		Invalidate(v)
		return
	}
	v.Anim = LongestAnimation(v.Anims)
	v.AnimIndex = AnimationIndex(v.Model, v.Anim)
	v.AnimPlaybackDuration = AnimClipSeconds(v.Anim)
	if !playback {
		v.Playing = false
	}
	v.Time = 0
	v.PoseValid = false
	v.LastFrame = time.Time{}
	Invalidate(v)
}

func AnimationIndex(model *uistate.WhmModel, anim *uistate.WhmAnimation) int {
	if model != nil && anim != nil {
		for i := range model.Animations {
			if &model.Animations[i] == anim {
				return i
			}
		}
	}
	return 0
}

func LongestAnimation(anims []*uistate.WhmAnimation) *uistate.WhmAnimation {
	var longest *uistate.WhmAnimation
	for _, anim := range anims {
		if anim != nil && (longest == nil || AnimClipSeconds(anim) > AnimClipSeconds(longest)) {
			longest = anim
		}
	}
	return longest
}

// AnimFramesPerSecond derives a playback rate from a clip. Dawn of War stores a
// frame count and a duration but no explicit rate, so the duration wins and a
// game-like 20 fps is the fallback.
func AnimFramesPerSecond(anim *uistate.WhmAnimation) float64 {
	if anim == nil || anim.NumFrames <= 0 {
		return 20
	}
	if anim.Duration > 0 {
		return float64(anim.NumFrames) / float64(anim.Duration)
	}
	return 20
}

// AnimClipSeconds is how long one pass of a clip takes, which is what the .whe
// timelines count their event moments in: necron_monolith's add_on_3 is 3513
// frames long and its events are written at 43 to 51, so they only mean
// anything against the 58.5 seconds the clip takes to play.
func AnimClipSeconds(anim *uistate.WhmAnimation) float64 {
	if anim == nil || anim.NumFrames <= 0 {
		return 1
	}
	if s := float64(anim.NumFrames) / AnimFramesPerSecond(anim); s > 0 {
		return s
	}
	return 1
}
