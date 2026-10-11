package engine

import (
	"math"

	"github.com/fogleman/fauxgl"
)

// Camera is the renderer-independent orbit-camera state used by both software
// and hardware renderers.
type Camera struct {
	Width, Height int
	Yaw, Pitch    float64
	Distance      float64
	Target        fauxgl.Vector
	Fov           float64
}

// ViewMatrix builds the clip-space transform for the Z-up orbit camera.
func ViewMatrix(opts Camera) fauxgl.Matrix {
	pitch := opts.Pitch
	if pitch > 1.5 {
		pitch = 1.5
	}
	if pitch < -1.5 {
		pitch = -1.5
	}
	dir := fauxgl.Vector{
		X: math.Cos(opts.Yaw) * math.Cos(pitch),
		Y: math.Sin(opts.Yaw) * math.Cos(pitch),
		Z: math.Sin(pitch),
	}
	distance := opts.Distance
	if distance <= 0 {
		distance = 1
	}
	eye := opts.Target.Add(dir.MulScalar(distance))
	view := fauxgl.LookAt(eye, opts.Target, fauxgl.Vector{Z: 1})
	near := math.Max(0.01, distance/1000)
	far := distance*10 + 1000
	return fauxgl.Perspective(opts.Fov, float64(opts.Width)/float64(opts.Height), near, far).Mul(view)
}

// EyePosition returns the camera's location in model space.
func EyePosition(opts Camera) fauxgl.Vector {
	pitch := opts.Pitch
	if pitch > 1.5 {
		pitch = 1.5
	}
	if pitch < -1.5 {
		pitch = -1.5
	}
	distance := opts.Distance
	if distance <= 0 {
		distance = 1
	}
	return fauxgl.Vector{
		X: opts.Target.X + math.Cos(opts.Yaw)*math.Cos(pitch)*distance,
		Y: opts.Target.Y + math.Sin(opts.Yaw)*math.Cos(pitch)*distance,
		Z: opts.Target.Z + math.Sin(pitch)*distance,
	}
}

// Project maps a model-space point to pixels, or nil when it lies behind or
// far outside the view.
func Project(view fauxgl.Matrix, p fauxgl.Vector, width, height int) *fauxgl.Vector {
	v := view.MulPositionW(p)
	if v.W <= 1e-6 {
		return nil
	}
	x := (v.X/v.W)*0.5 + 0.5
	y := (v.Y/v.W)*0.5 + 0.5
	if x < -2 || x > 3 || y < -2 || y > 3 {
		return nil
	}
	return &fauxgl.Vector{X: x * float64(width), Y: (1 - y) * float64(height)}
}
