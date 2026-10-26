package render

import (
	"math"

	engine "dow-mod-studio/internal/3d_engine"

	"github.com/fogleman/fauxgl"
)

// The FX overlay's camera-side math: both the CPU overlay in the view and the
// GPU renderer need where the eye is and which way it faces, derived from the
// same render request, so it lives beside RenderOptions rather than in either
// of the two callers.

// SpriteFallbackRadius is the size, in model units, given to a sprite whose
// file carries no Radius curve.
const SpriteFallbackRadius = 0.05

// FxCameraBasis is where the camera's right and up point in model space, for
// the same orbit BuildViewMatrix sets up. Models are authored Z up, so that
// is the up the look is taken against.
//
// The two are the same axes the view matrix is built from, and the order of
// the cross product is what makes them agree: the eye sits along dir, so
// looking at the model means looking back along -dir, and the camera's right
// is worldUp x -dir. Taking dir x worldUp instead hands back the negative of
// it — the camera's left — which squares every billboard to a frame rolled a
// half turn against the one it is being viewed through. A round puff is
// symmetric enough to hide that, but a sprite with a direction in it, which is
// what a beam's map is, comes out upside down and swings the wrong way as the
// camera orbits.
func FxCameraBasis(opts RenderOptions) (right, up fauxgl.Vector) {
	pitch := engine.ClampFloat64(opts.Pitch, -1.5, 1.5)
	dir := fauxgl.Vector{
		X: math.Cos(opts.Yaw) * math.Cos(pitch),
		Y: math.Sin(opts.Yaw) * math.Cos(pitch),
		Z: math.Sin(pitch),
	}
	worldUp := fauxgl.Vector{X: 0, Y: 0, Z: 1}
	right = worldUp.Cross(dir).Normalize()
	if n := right.Length(); n < 1e-6 {
		// Looking straight down the up axis: any sideways direction will
		// do, and the frame itself is edge on anyway.
		right = fauxgl.Vector{X: 1}
	}
	up = dir.Cross(right).Normalize()
	return right, up
}

// FxEyePosition is where the camera is in model space, for the same orbit
// BuildViewMatrix sets up. A beam needs it to turn its cross-section towards
// the eye: a ribbon has a direction but no idea which way it is being looked
// at, and a beam drawn flat to the world disappears edge on.
func FxEyePosition(opts RenderOptions) fauxgl.Vector {
	return engine.EyePosition(engine.Camera{Yaw: opts.Yaw, Pitch: opts.Pitch, Distance: opts.Distance, Target: opts.Target})
}
