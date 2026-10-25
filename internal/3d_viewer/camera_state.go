package viewer

import (
	"math"

	"github.com/fogleman/fauxgl"
)

// CameraState is the orbit camera and framing state shared by the model-viewer
// canvas and the object viewport. It contains no application or Gio widgets.
type CameraState struct {
	Yaw         float64
	Pitch       float64
	Distance    float64
	Center      fauxgl.Vector
	FitDistance float64
	// MinDistance and MaxDistance bound the zoom. Zero derives the range
	// from FitDistance (15%..6×), which is right for a model; a scene the
	// size of a map sets its own so the wheel can reach a close orbit and
	// still fit the whole thing.
	MinDistance, MaxDistance float64
	// DefaultDistance is what ResetCamera puts the camera back at; zero
	// resets to the FitDistance framing.
	DefaultDistance float64
	FOV             float64
	ActiveCamera    string
}

// zoomLimits is the range the camera distance is kept inside.
func (camera CameraState) zoomLimits() (float64, float64) {
	lo, hi := camera.FitDistance*0.15, camera.FitDistance*6
	if camera.MinDistance > 0 {
		lo = camera.MinDistance
	}
	if camera.MaxDistance > 0 {
		hi = camera.MaxDistance
	}
	if hi < lo {
		hi = lo
	}
	return lo, hi
}

// ResetCamera returns the default orbit view while retaining the model's fit.
func ResetCamera(camera CameraState) CameraState {
	camera.Yaw, camera.Pitch = 0.7, 0.2
	camera.Distance = camera.FitDistance
	if camera.DefaultDistance > 0 {
		camera.Distance = camera.DefaultDistance
	}
	camera.FOV = 45
	camera.ActiveCamera = ""
	return camera
}

// LookThroughCamera orients the orbit camera to an eye and focus point.
func LookThroughCamera(camera CameraState, eye, focus fauxgl.Vector, fov float32, name string) CameraState {
	dir := eye.Sub(focus)
	distance := dir.Length()
	if distance < 1e-6 {
		distance = camera.FitDistance
	} else {
		dir = dir.DivScalar(distance)
	}
	camera.Center = focus
	camera.Yaw = math.Atan2(dir.Y, dir.X)
	pitch := math.Asin(clamp(dir.Z, -1, 1))
	camera.Pitch = clamp(pitch, -1.45, 1.45)
	lo, hi := camera.zoomLimits()
	camera.Distance = clamp(distance, lo, hi)
	if fov > 1 && fov < 170 {
		camera.FOV = float64(fov)
	}
	camera.ActiveCamera = name
	return camera
}

// ZoomBy applies a camera zoom while keeping it in the viewer's safe range.
func ZoomBy(camera CameraState, factor float64) CameraState {
	if factor <= 0 || factor == 1 {
		return camera
	}
	lo, hi := camera.zoomLimits()
	camera.Distance = clamp(camera.Distance*factor, lo, hi)
	return camera
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
