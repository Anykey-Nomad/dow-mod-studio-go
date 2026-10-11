package engine

import (
	"math"

	"dow-mod-studio/internal/whm"
	"github.com/fogleman/fauxgl"
)

// OverlayOptions are the camera and selection inputs needed to build model
// debug geometry; UI state stays outside this package.
type OverlayOptions struct {
	Camera
	Skeleton   bool
	Marker     *int
	PickedBone *int
}

// OverlayLine is a model-space segment shared by software and hardware renderers.
type OverlayLine struct {
	From, To   fauxgl.Vector
	HalfWidth  float64
	R, G, B, A float64
}

var (
	SkeletonColor      = [4]float64{0.15, 0.85, 0.35, 1}
	BonePickColor      = [4]float64{1, 0.62, 0.15, 1}
	MarkerXColor       = [4]float64{1, 0.25, 0.25, 1}
	MarkerYColor       = [4]float64{0.3, 1, 0.3, 1}
	MarkerZColor       = [4]float64{0.4, 0.55, 1, 1}
	MarkerForwardColor = [4]float64{1, 0.85, 0.2, 1}
)

const (
	skeletonPixels        = 2.0
	bonePickPixels        = 3.0
	bonePickArmPixels     = 12.0
	markerAxisPixels      = 1.5
	markerForwardPixels   = 2.5
	MarkerAxisWorldLength = 0.5
)

// OverlayHalfWidth converts a pixel width to model units at the camera distance.
func OverlayHalfWidth(opts OverlayOptions, pixels float64) float64 {
	fov := opts.Fov
	if fov <= 0 {
		fov = 45
	}
	height := float64(opts.Height)
	if height < 1 {
		height = 1
	}
	distance := opts.Distance
	if distance <= 0 {
		distance = 1
	}
	return math.Tan(fov*math.Pi/360) * distance * pixels / height
}

// OverlayLines builds the skeleton and selected marker/bone geometry.
func OverlayLines(model *whm.Model, pose whm.Pose, opts OverlayOptions) []OverlayLine {
	if model == nil {
		return nil
	}
	var out []OverlayLine
	if opts.Skeleton && len(model.Bones) > 0 {
		half := OverlayHalfWidth(opts, skeletonPixels)
		for i, bone := range model.Bones {
			if bone.Parent < 0 || int(bone.Parent) >= len(pose.Bones) || i >= len(pose.Bones) {
				continue
			}
			parent := pose.Bones[bone.Parent].MulPosition(fauxgl.Vector{})
			origin := pose.Bones[i].MulPosition(fauxgl.Vector{})
			out = append(out, OverlayLine{From: parent, To: origin, HalfWidth: half,
				R: SkeletonColor[0], G: SkeletonColor[1], B: SkeletonColor[2], A: SkeletonColor[3]})
		}
	}
	if opts.PickedBone != nil && model.Bones != nil && pose.Bones != nil {
		i := *opts.PickedBone
		if i >= 0 && i < len(model.Bones) && i < len(pose.Bones) {
			bone := model.Bones[i]
			half := OverlayHalfWidth(opts, bonePickPixels)
			seg := func(from, to fauxgl.Vector) OverlayLine {
				return OverlayLine{From: from, To: to, HalfWidth: half,
					R: BonePickColor[0], G: BonePickColor[1], B: BonePickColor[2], A: BonePickColor[3]}
			}
			origin := pose.Bones[i].MulPosition(fauxgl.Vector{})
			if bone.Parent >= 0 && int(bone.Parent) < len(pose.Bones) {
				parent := pose.Bones[bone.Parent].MulPosition(fauxgl.Vector{})
				out = append(out, seg(parent, origin))
			} else {
				arm := OverlayHalfWidth(opts, bonePickArmPixels)
				for _, axis := range []fauxgl.Vector{{X: 1}, {Y: 1}, {Z: 1}} {
					out = append(out, seg(origin.Sub(axis.MulScalar(arm)), origin.Add(axis.MulScalar(arm))))
				}
			}
		}
	}
	return append(out, MarkerOverlayLines(model, pose, opts)...)
}

// MarkerOverlayLines builds the selected marker's axis frame and forward arrow.
func MarkerOverlayLines(model *whm.Model, pose whm.Pose, opts OverlayOptions) []OverlayLine {
	if model == nil || opts.Marker == nil {
		return nil
	}
	i := *opts.Marker
	if i < 0 || i >= len(model.Markers) {
		return nil
	}
	at, ok := model.MarkerWorld(i, pose, true)
	if !ok {
		return nil
	}
	length := MarkerAxisWorldLength
	axisHalf := OverlayHalfWidth(opts, markerAxisPixels)
	forwardHalf := OverlayHalfWidth(opts, markerForwardPixels)
	var out []OverlayLine
	seg := func(dir fauxgl.Vector, half float64, col [4]float64) {
		if dir.Length() < 1e-6 {
			return
		}
		out = append(out, OverlayLine{From: at, To: at.Add(dir.Normalize().MulScalar(length)), HalfWidth: half,
			R: col[0], G: col[1], B: col[2], A: col[3]})
	}
	x, y, z, axesOK := model.MarkerFrameAxes(i, pose, true)
	if axesOK {
		seg(x, axisHalf, MarkerXColor)
		seg(x.MulScalar(-1), axisHalf, MarkerXColor)
		seg(y, axisHalf, MarkerYColor)
		seg(y.MulScalar(-1), axisHalf, MarkerYColor)
		seg(z, axisHalf, MarkerZColor)
		seg(z.MulScalar(-1), axisHalf, MarkerZColor)
	}
	forward, ok := model.MarkerAxis(i, pose, true)
	if !ok {
		return out
	}
	tip := at.Add(forward.MulScalar(length))
	out = append(out, OverlayLine{From: at, To: tip, HalfWidth: forwardHalf,
		R: MarkerForwardColor[0], G: MarkerForwardColor[1], B: MarkerForwardColor[2], A: MarkerForwardColor[3]})
	eye := EyePosition(opts.Camera)
	if across := tip.Sub(eye).Cross(forward); across.Length() > 1e-6 {
		across = across.Normalize()
		wing := length * 0.3
		base := tip.Sub(forward.MulScalar(wing))
		for _, sign := range []float64{1, -1} {
			out = append(out, OverlayLine{From: base.Add(across.MulScalar(wing * sign)), To: tip, HalfWidth: forwardHalf,
				R: MarkerForwardColor[0], G: MarkerForwardColor[1], B: MarkerForwardColor[2], A: MarkerForwardColor[3]})
		}
	}
	return out
}
