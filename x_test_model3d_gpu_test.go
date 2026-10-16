//go:build windows || linux

package main

import (
	"image"
	"image/color"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"dow-mod-studio/internal/whm"
)

// newLayoutContextForTest builds the minimal context Paint needs, which is only
// the op stream, the constraints and the metric scale.
func newLayoutContextForTest(ops *op.Ops, w, h int) layout.Context {
	return layout.Context{
		Ops:         ops,
		Constraints: layout.Exact(image.Pt(w, h)),
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
	}
}

// The clear colour of a pass is the panel background.
const (
	bgR, bgG, bgB = 0x1c, 0x1e, 0x24
)

// findDowFile looks a file up by base name anywhere under the installed race
// folders, because the game scatters the same kind of file across troops,
// structures and shared texture folders.
func findDowFile(name string) []byte {
	want := strings.ToLower(name)
	var found []byte
	filepath.WalkDir(dowRacesDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || found != nil {
			return nil //nolint:nilerr // an unreadable folder just means no match
		}
		if d.IsDir() || !strings.EqualFold(d.Name(), want) {
			return nil
		}
		if data, err := os.ReadFile(path); err == nil {
			found = data
		}
		return nil
	})
	return found
}

// requireGpuTestModel reads a model from the game install, skipping the test
// when there is none.
func requireGpuTestModel(t *testing.T, name string) *WhmModel {
	t.Helper()
	raw := findDowFile(name)
	if raw == nil {
		t.Skipf("no %s under %s", name, dowRacesDir)
	}
	model, err := ParseWhmModel(raw)
	if err != nil {
		t.Fatalf("ParseWhmModel(%s): %v", name, err)
	}
	return model
}

// gpuTestOptions frames a model the way a freshly opened viewer does.
func gpuTestOptions(model *WhmModel, w, h int) RenderOptions {
	center, dist := model.ModelCenter()
	return RenderOptions{
		Width:     w,
		Height:    h,
		Yaw:       0.7,
		Pitch:     0.2,
		Distance:  dist,
		Target:    center,
		Fov:       45,
		Ambient:   0.32,
		Diffuse:   0.68,
		BaseColor: rgbaWhite,
	}
}

// coveredFraction counts the pixels that are not the clear colour, which is
// how much of the frame the model actually reached.
func coveredFraction(px []byte, w, h int) float64 {
	hits := 0
	for i := 0; i+3 < len(px); i += 4 {
		if px[i] != bgR || px[i+1] != bgG || px[i+2] != bgB {
			hits++
		}
	}
	return float64(hits) / float64(w*h)
}

// distinctColours counts the unique opaque colours in a frame, which is how a
// textured model tells itself apart from one painted in a flat colour.
func distinctColours(px []byte) int {
	seen := map[uint32]bool{}
	for i := 0; i+3 < len(px); i += 4 {
		if px[i] == bgR && px[i+1] == bgG && px[i+2] == bgB {
			continue
		}
		seen[uint32(px[i])<<16|uint32(px[i+1])<<8|uint32(px[i+2])] = true
	}
	return len(seen)
}

// findRsh locates a material's .rsh by base name: a .whm stores a full virtual
// path, but the file itself may sit in a different mod than the model.
func findRsh(material string) []byte {
	if material == "" {
		return nil
	}
	return findDowFile(filepath.Base(filepath.FromSlash(material)) + ".rsh")
}

// findMaterialWithTexture returns the first material of a model whose .rsh is
// actually installed, since a model can reference a texture the install does
// not ship.
func findMaterialWithTexture(model *WhmModel) (string, []byte) {
	for _, m := range model.Meshes {
		if data := findRsh(m.Material); data != nil {
			return m.Material, data
		}
	}
	return "", nil
}

// rgbaPixels copies an image into the tightly packed layout the readback
// helpers expect.
func rgbaPixels(img *image.RGBA) []byte {
	b := img.Bounds()
	out := make([]byte, 0, b.Dx()*b.Dy()*4)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		out = append(out, img.Pix[y*img.Stride:y*img.Stride+b.Dx()*4]...)
	}
	return out
}

// singleQuad is one flat quad across the YZ plane with its normal along the X
// axis, which is where the orbit camera sits with no yaw and no pitch.
func singleQuad(t *testing.T, outwards bool) *WhmModel {
	t.Helper()
	m := &WhmModel{Model: whm.Model{Name: "quad"}}
	for _, p := range [4][3]float32{
		{0, -0.8, -0.8}, {0, 0.8, -0.8}, {0, 0.8, 0.8}, {0, -0.8, 0.8},
	} {
		m.Vertices = append(m.Vertices, WhmVertex{
			Pos:    p,
			Normal: [3]float32{1, 0, 0},
			Bones:  [4]int32{-1, -1, -1, -1},
		})
	}
	// Counter clockwise seen from the front, which puts the outside of the
	// quad on the side the camera is on.
	idx := []uint32{0, 1, 2, 0, 2, 3}
	if !outwards {
		idx = []uint32{0, 2, 1, 0, 3, 2}
	}
	m.Meshes = append(m.Meshes, WhmMesh{Name: "quad", Indices: idx, SingleBone: -1})
	m.Bones = []WhmBone{{Name: "root", Parent: -1}}
	return m
}

// covered is whether a pixel is clear of the background a frame is cleared to.
// The arithmetic is done on ints: the background is darker than the threshold
// in the blue channel, and unsigned bytes would count that as a large number.
func covered(c color.RGBA) bool {
	return int(c.R)-0x1c > 12 || int(c.G)-0x1e > 12 || int(c.B)-0x24 > 12
}

// openBoxModel is a box with its top and bottom left off.
func openBoxModel(t *testing.T) *WhmModel {
	t.Helper()
	m := &WhmModel{Model: whm.Model{Name: "open_box"}}
	min, max := [3]float32{-1, -1, -1}, [3]float32{1, 1, 1}
	corners := [8][3]float32{
		{min[0], min[1], min[2]}, {max[0], min[1], min[2]}, {max[0], max[1], min[2]}, {min[0], max[1], min[2]},
		{min[0], min[1], max[2]}, {max[0], min[1], max[2]}, {max[0], max[1], max[2]}, {min[0], max[1], max[2]},
	}
	centre := [3]float32{0, 0, 0}
	for _, c := range corners {
		n := [3]float32{c[0] - centre[0], c[1] - centre[1], c[2] - centre[2]}
		l := float32(sqrt(n[0]*n[0] + n[1]*n[1] + n[2]*n[2]))
		m.Vertices = append(m.Vertices, WhmVertex{
			Pos:    c,
			Normal: [3]float32{n[0] / l, n[1] / l, n[2] / l},
			Bones:  [4]int32{-1, -1, -1, -1},
		})
	}
	// The four sides only. The opening is what the frame looks through.
	faces := [][3]int{
		{0, 1, 5}, {0, 5, 4}, // front
		{1, 2, 6}, {1, 6, 5}, // right
		{2, 3, 7}, {2, 7, 6}, // back
		{3, 0, 4}, {3, 4, 7}, // left
	}
	var idx []uint32
	for _, f := range faces {
		if facePointsOutwards(m.Vertices[f[0]], m.Vertices[f[1]], m.Vertices[f[2]]) {
			idx = append(idx, uint32(f[0]), uint32(f[1]), uint32(f[2]))
		} else {
			idx = append(idx, uint32(f[0]), uint32(f[2]), uint32(f[1]))
		}
	}
	m.Meshes = append(m.Meshes, WhmMesh{Name: "walls", Indices: idx, SingleBone: -1})
	m.Bones = []WhmBone{{Name: "root", Parent: -1}}
	return m
}

// twoBoxModel is two cubes far apart, as two sub meshes. Which piece is drawn
// is therefore obvious at a glance, and so is which side of each is.
func twoBoxModel(t *testing.T) *WhmModel {
	t.Helper()
	m := &WhmModel{Model: whm.Model{Name: "two_boxes"}}
	add := func(name string, min, max [3]float32) {
		base := uint32(len(m.Vertices))
		// Every face of a cube as two triangles, wound so that the normal
		// each one carries points away from the middle of it.
		corners := [8][3]float32{
			{min[0], min[1], min[2]}, {max[0], min[1], min[2]}, {max[0], max[1], min[2]}, {min[0], max[1], min[2]},
			{min[0], min[1], max[2]}, {max[0], min[1], max[2]}, {max[0], max[1], max[2]}, {min[0], max[1], max[2]},
		}
		centre := [3]float32{(min[0] + max[0]) / 2, (min[1] + max[1]) / 2, (min[2] + max[2]) / 2}
		for i, c := range corners {
			n := [3]float32{c[0] - centre[0], c[1] - centre[1], c[2] - centre[2]}
			length := float32(1)
			if l := n[0]*n[0] + n[1]*n[1] + n[2]*n[2]; l > 0 {
				length = float32(sqrt(l))
			}
			m.Vertices = append(m.Vertices, WhmVertex{
				Pos:    c,
				Normal: [3]float32{n[0] / length, n[1] / length, n[2] / length},
				UV:     [2]float32{0, 0},
				Bones:  [4]int32{-1, -1, -1, -1},
			})
			_ = i
		}
		faces := [][3]int{
			{0, 2, 1}, {0, 3, 2}, // bottom
			{4, 5, 6}, {4, 6, 7}, // top
			{0, 1, 5}, {0, 5, 4}, // front
			{1, 2, 6}, {1, 6, 5}, // right
			{2, 3, 7}, {2, 7, 6}, // back
			{3, 0, 4}, {3, 4, 7}, // left
		}
		var idx []uint32
		for _, f := range faces {
			// Wound so that cross(b-a, c-a) points along the face normal.
			if facePointsOutwards(m.Vertices[base+uint32(f[0])], m.Vertices[base+uint32(f[1])], m.Vertices[base+uint32(f[2])]) {
				idx = append(idx, base+uint32(f[0]), base+uint32(f[1]), base+uint32(f[2]))
			} else {
				idx = append(idx, base+uint32(f[0]), base+uint32(f[2]), base+uint32(f[1]))
			}
		}
		m.Meshes = append(m.Meshes, WhmMesh{Name: name, Indices: idx, SingleBone: -1})
	}
	add("left", [3]float32{-1.4, -0.5, -0.5}, [3]float32{-0.4, 0.5, 0.5})
	add("right", [3]float32{0.4, -0.5, -0.5}, [3]float32{1.4, 0.5, 0.5})
	m.Bones = []WhmBone{{Name: "root", Parent: -1}}
	return m
}

// facePointsOutwards reports whether the winding of a triangle agrees with the
// normal its vertices carry.
func facePointsOutwards(a, b, c WhmVertex) bool {
	u1 := [3]float64{float64(b.Pos[0] - a.Pos[0]), float64(b.Pos[1] - a.Pos[1]), float64(b.Pos[2] - a.Pos[2])}
	u2 := [3]float64{float64(c.Pos[0] - a.Pos[0]), float64(c.Pos[1] - a.Pos[1]), float64(c.Pos[2] - a.Pos[2])}
	g := [3]float64{
		u1[1]*u2[2] - u1[2]*u2[1],
		u1[2]*u2[0] - u1[0]*u2[2],
		u1[0]*u2[1] - u1[1]*u2[0],
	}
	n := [3]float64{
		float64(a.Normal[0]) + float64(b.Normal[0]) + float64(c.Normal[0]),
		float64(a.Normal[1]) + float64(b.Normal[1]) + float64(c.Normal[1]),
		float64(a.Normal[2]) + float64(b.Normal[2]) + float64(c.Normal[2]),
	}
	return g[0]*n[0]+g[1]*n[1]+g[2]*n[2] > 0
}

// coveredPixels counts the pixels of a frame that are not the background the
// renderer clears to.
func coveredPixels(img *image.RGBA, area image.Rectangle) int {
	n := 0
	for y := area.Min.Y; y < area.Max.Y; y++ {
		for x := area.Min.X; x < area.Max.X; x++ {
			if covered(img.RGBAAt(x, y)) {
				n++
			}
		}
	}
	return n
}

func sqrt(v float32) float64 { return math.Sqrt(float64(v)) }
