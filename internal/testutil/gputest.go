package testutil

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	engine "dow-mod-studio/internal/3d_engine"
	"dow-mod-studio/internal/app/render"
	"dow-mod-studio/internal/whm"
)

// The real game install the file-reading tests read from. Tests skip when it
// is not there, so a machine without the game still passes.
const (
	DowInstallDir = `C:\Program Files\Steam\steamapps\common\Dawn of War Definitive Edition\UltimateApocalypse_THB_test\data`
	DowRacesDir   = DowInstallDir + `\art\ebps\races`
)

// The clear colour of a renderer pass is the panel background, which is what
// tells a drawn pixel from an empty one.
const (
	bgR, bgG, bgB = 0x1c, 0x1e, 0x24
)

// FindDowFile looks a file up by base name anywhere under the installed race
// folders, because the game scatters the same kind of file across troops,
// structures and shared texture folders.
func FindDowFile(name string) []byte {
	want := strings.ToLower(name)
	var found []byte
	filepath.WalkDir(DowRacesDir, func(path string, d fs.DirEntry, err error) error {
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

// RequireGpuTestModel reads a model from the game install, skipping the test
// when there is none.
func RequireGpuTestModel(t *testing.T, name string) *whm.Model {
	t.Helper()
	raw := FindDowFile(name)
	if raw == nil {
		t.Skipf("no %s under %s", name, DowRacesDir)
	}
	model, err := whm.Parse(raw)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	return model
}

// GpuTestOptions frames a model the way a freshly opened viewer does.
func GpuTestOptions(model *whm.Model, w, h int) render.RenderOptions {
	center, dist := engine.ModelCenter(model)
	return render.RenderOptions{
		Width:     w,
		Height:    h,
		Yaw:       0.7,
		Pitch:     0.2,
		Distance:  dist,
		Target:    center,
		Fov:       45,
		Ambient:   0.32,
		Diffuse:   0.68,
		BaseColor: render.RgbaWhite,
	}
}

// DistinctColours counts the unique opaque colours in a frame, which is how a
// textured model tells itself apart from one painted in a flat colour.
func DistinctColours(px []byte) int {
	seen := map[uint32]bool{}
	for i := 0; i+3 < len(px); i += 4 {
		if px[i] == bgR && px[i+1] == bgG && px[i+2] == bgB {
			continue
		}
		seen[uint32(px[i])<<16|uint32(px[i+1])<<8|uint32(px[i+2])] = true
	}
	return len(seen)
}

// FacePointsOutwards reports whether the winding of a triangle agrees with the
// normal its vertices carry.
func FacePointsOutwards(a, b, c whm.Vertex) bool {
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
