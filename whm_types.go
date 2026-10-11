package main

import "dow-mod-studio/internal/whm"

// The .whm model parser lives in internal/whm. The other types are aliases,
// so the viewer, the pose editor and the SGM converter read the same types
// they did before the move.
//
// WhmModel is a wrapper rather than an alias because the root adds methods to
// the model — pose, markers, alpha — and Go does not allow methods on a
// non-local type. The embedded field promotes every field the parser fills,
// so the rest of the code reads m.Bones and friends unchanged.
type WhmModel struct {
	whm.Model
}

type WhmPose = whm.Pose

var boneRootFrame = whm.RootFrame

type (
	WhmVertex       = whm.Vertex
	WhmMesh         = whm.Mesh
	WhmBone         = whm.Bone
	WhmMarker       = whm.Marker
	WhmCamera       = whm.Camera
	WhmKey3         = whm.Key3
	WhmKey4         = whm.Key4
	WhmTrack        = whm.Track
	WhmTrackKind    = whm.TrackKind
	WhmMeshTrack    = whm.MeshTrack
	WhmTextureTrack = whm.TextureTrack
	WhmAnimation    = whm.Animation
)

const (
	TrackMeshVisibility = whm.TrackMeshVisibility
	TrackTextureUV      = whm.TrackTextureUV
)

func ParseWhmModel(data []byte) (*WhmModel, error) {
	m, err := whm.Parse(data)
	if err != nil {
		return nil, err
	}
	return &WhmModel{Model: *m}, nil
}
