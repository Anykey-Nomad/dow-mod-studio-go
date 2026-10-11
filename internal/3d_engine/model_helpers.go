package engine

import (
	"github.com/fogleman/fauxgl"

	"dow-mod-studio/internal/whm"
)

// ModelCenter returns the midpoint of the model bounds and a distance that
// frames the whole model. Empty or degenerate models get a stable fallback.
func ModelCenter(model *whm.Model) (fauxgl.Vector, float64) {
	if model == nil || len(model.Vertices) == 0 {
		return fauxgl.Vector{}, 1
	}
	min := fauxgl.Vector{X: float64(model.BoundMin[0]), Y: float64(model.BoundMin[1]), Z: float64(model.BoundMin[2])}
	max := fauxgl.Vector{X: float64(model.BoundMax[0]), Y: float64(model.BoundMax[1]), Z: float64(model.BoundMax[2])}
	center := min.Add(max).DivScalar(2)
	size := max.Sub(min).Length()
	if size <= 1e-6 {
		size = 1
	}
	return center, size * 1.4
}

// UVBounds returns the extent covered by the model's vertex texture
// coordinates, or ok=false when it has no vertices.
func UVBounds(model *whm.Model) (minU, minV, maxU, maxV float32, ok bool) {
	if model == nil || len(model.Vertices) == 0 {
		return 0, 0, 0, 0, false
	}
	minU, minV = model.Vertices[0].UV[0], model.Vertices[0].UV[1]
	maxU, maxV = minU, minV
	for i := range model.Vertices {
		u, v := model.Vertices[i].UV[0], model.Vertices[i].UV[1]
		if u < minU {
			minU = u
		}
		if u > maxU {
			maxU = u
		}
		if v < minV {
			minV = v
		}
		if v > maxV {
			maxV = v
		}
	}
	return minU, minV, maxU, maxV, true
}

// MeshAlpha returns the visibility alpha for a named mesh, defaulting to 1.
func MeshAlpha(alphas map[string]float32, name string) float32 {
	if alpha, ok := alphas[name]; ok {
		return alpha
	}
	return 1
}

// MeshAlphaByte rounds a visibility alpha to the nearest byte and clamps it.
func MeshAlphaByte(alpha float32) uint8 {
	if alpha <= 0 {
		return 0
	}
	if alpha >= 1 {
		return 255
	}
	return uint8(float64(alpha)*255 + 0.5)
}
