package main

import (
	"dow-mod-studio/internal/3d_engine"
	"dow-mod-studio/internal/whm"
	"github.com/fogleman/fauxgl"
)

type modelOverlayLine = engine.OverlayLine

var (
	modelSkeletonColor  = engine.SkeletonColor
	modelBonePickColor  = engine.BonePickColor
	modelMarkerXColor   = engine.MarkerXColor
	modelMarkerYColor   = engine.MarkerYColor
	modelMarkerZColor   = engine.MarkerZColor
	modelMarkerFwdColor = engine.MarkerForwardColor
)

const markerArrowWorldLength = engine.MarkerAxisWorldLength

func pickedMarker(index int) *int {
	if index < 0 {
		return nil
	}
	return &index
}

func pickedBone(index int) *int {
	if index < 0 {
		return nil
	}
	return &index
}

func modelOverlayHalfWidth(opts RenderOptions, pixels float64) float64 {
	return engine.OverlayHalfWidth(engine.OverlayOptions{Camera: engine.Camera{
		Width: opts.Width, Height: opts.Height, Yaw: opts.Yaw, Pitch: opts.Pitch,
		Distance: opts.Distance, Target: opts.Target, Fov: opts.Fov,
	}}, pixels)
}

func modelOverlayLines(model *WhmModel, pose WhmPose, opts RenderOptions) []modelOverlayLine {
	if model == nil {
		return nil
	}
	eo := engine.OverlayOptions{Camera: engine.Camera{
		Width: opts.Width, Height: opts.Height, Yaw: opts.Yaw, Pitch: opts.Pitch,
		Distance: opts.Distance, Target: opts.Target, Fov: opts.Fov,
	}, Skeleton: opts.Skeleton, Marker: opts.Marker, PickedBone: opts.PickedBone}
	return engine.OverlayLines(&model.Model, pose, eo)
}

func modelMarkerOverlayLines(model *WhmModel, pose WhmPose, opts RenderOptions) []modelOverlayLine {
	if model == nil {
		return nil
	}
	eo := engine.OverlayOptions{Camera: engine.Camera{
		Width: opts.Width, Height: opts.Height, Yaw: opts.Yaw, Pitch: opts.Pitch,
		Distance: opts.Distance, Target: opts.Target, Fov: opts.Fov,
	}, Marker: opts.Marker}
	return engine.MarkerOverlayLines(&model.Model, pose, eo)
}

