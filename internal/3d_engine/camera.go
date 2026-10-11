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
	dir := OrbitDirection(opts)
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

// OrbitDirection returns the unit vector from the target to the camera.
func OrbitDirection(opts Camera) fauxgl.Vector {
	pitch := opts.Pitch
	if pitch > 1.5 {
		pitch = 1.5
	}
	if pitch < -1.5 {
		pitch = -1.5
	}
	return fauxgl.Vector{
		X: math.Cos(opts.Yaw) * math.Cos(pitch),
		Y: math.Sin(opts.Yaw) * math.Cos(pitch),
		Z: math.Sin(pitch),
	}
}

// ViewDirection returns the direction the camera looks in model space.
func ViewDirection(opts Camera) fauxgl.Vector {
	dir := OrbitDirection(opts)
	if n := dir.Length(); n > 1e-9 {
		dir = dir.MulScalar(1 / n)
	}
	return dir.MulScalar(-1)
}

// AxisAngles returns camera-to-axis angles in degrees, in X/Y/Z order.
func AxisAngles(opts Camera) [3]float64 {
	view := ViewDirection(opts)
	angle := func(axis fauxgl.Vector) float64 {
		dot := view.Dot(axis)
		if dot > 1 {
			dot = 1
		}
		if dot < -1 {
			dot = -1
		}
		return math.Acos(dot) * 180 / math.Pi
	}
	return [3]float64{
		angle(fauxgl.Vector{X: 1}),
		angle(fauxgl.Vector{Y: 1}),
		angle(fauxgl.Vector{Z: 1}),
	}
}

// EyePosition returns the camera's location in model space.
func EyePosition(opts Camera) fauxgl.Vector {
	distance := opts.Distance
	if distance <= 0 {
		distance = 1
	}
	return opts.Target.Add(OrbitDirection(opts).MulScalar(distance))
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
