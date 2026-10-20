package sgb

import (
	"math"

	"dow-mod-studio/internal/whm"
	"github.com/fogleman/fauxgl"
)

// Scene placement: where one entity's model sits in the renderer's space.
//
// The scenario stores an entity as a position (x, y, z) with y the height,
// plus a rotation about the vertical axis recovered as Heading. The renderer
// is Z up with the map plane on X/Y in the same world frame the decals and
// the terrain use, so a placement is the model scaled in its own axes,
// rotated by the heading about up, and moved to (x, z, y).
//

// Placement is one entity's placement in renderer space.
type Placement struct {
	X, Y, Z float64 // world position; Y is up (the scenario's z)
	Heading float64 // rotation about up, radians
	ScaleX  float64
	ScaleY  float64
	ScaleZ  float64
}

// PlacementOf returns the default placement of a parsed entity: position
// swapped into renderer space, unit scale.
func (e Entity) PlacementOf() Placement {
	return Placement{
		X: float64(e.X), Y: float64(e.Z), Z: float64(e.Y),
		Heading: float64(e.Heading),
		ScaleX:  1, ScaleY: 1, ScaleZ: 1,
	}
}

// AppendEntity appends a static copy of src's geometry, placed by p, to dst.
// Animated entity instances instead use EntityModelMatrix and retain skin data.
// meshName names the appended sub mesh; the caller makes it unique.
func AppendEntity(dst *whm.Model, src *whm.Model, p Placement, meshName string) {
	AppendEntityWithPose(dst, src, p, meshName, src.RestPose(), false)
}

// AppendEntityWithPose bakes a pose into the merged scene. When animated is
// true it keeps skin influences and appends a separately addressed skeleton.
func AppendEntityWithPose(dst *whm.Model, src *whm.Model, p Placement, meshName string, pose whm.Pose, animated bool) int {
	if dst == nil || src == nil || len(src.Vertices) == 0 || len(src.Meshes) == 0 {
		return -1
	}
	base := uint32(len(dst.Vertices))
	boneBase := len(dst.Bones)
	if animated {
		dst.Bones = append(dst.Bones, src.Bones...)
		for i := range src.Bones {
			if src.Bones[i].Parent >= 0 {
				dst.Bones[boneBase+i].Parent += boneBase
			}
		}
	}
	ch, sh := math.Cos(p.Heading), math.Sin(p.Heading)
	sx, sy, sz := p.ScaleX, p.ScaleY, p.ScaleZ
	if sx == 0 {
		sx = 1
	}
	if sy == 0 {
		sy = 1
	}
	if sz == 0 {
		sz = 1
	}
	for i := range src.Vertices {
		pos, normal := whm.SkinnedVertex(src, pose, &src.Vertices[i])
		if animated {
			pos = fauxgl.Vector{X: float64(src.Vertices[i].Pos[0]), Y: float64(src.Vertices[i].Pos[1]), Z: float64(src.Vertices[i].Pos[2])}
			normal = fauxgl.Vector{X: float64(src.Vertices[i].Normal[0]), Y: float64(src.Vertices[i].Normal[1]), Z: float64(src.Vertices[i].Normal[2])}
		}
		x, y, z := pos.X*sx, pos.Y*sy, pos.Z*sz
		nx, ny, nz := normal.X/sx, normal.Y/sy, normal.Z/sz
		vertex := whm.Vertex{
			Pos: [3]float32{
				float32(ch*x - sh*y + p.X),
				float32(sh*x + ch*y + p.Y),
				float32(z + p.Z),
			},
			Normal: [3]float32{float32(nx*ch - ny*sh), float32(nx*sh + ny*ch), float32(nz)},
			UV:     src.Vertices[i].UV,
		}
		if animated {
			vertex.Bones = src.Vertices[i].Bones
			vertex.Weights = src.Vertices[i].Weights
			for slot, bone := range vertex.Bones {
				if bone >= 0 {
					vertex.Bones[slot] += int32(boneBase)
				}
			}
		} else {
			vertex.Bones = [4]int32{-1, -1, -1, -1}
		}
		dst.Vertices = append(dst.Vertices, vertex)
	}
	for _, mesh := range src.Meshes {
		if len(mesh.Indices) == 0 {
			continue
		}
		idx := make([]uint32, len(mesh.Indices))
		for i, gi := range mesh.Indices {
			if int(gi) < len(src.Vertices) {
				idx[i] = base + gi
			} else {
				idx[i] = base
			}
		}
		dst.Meshes = append(dst.Meshes, whm.Mesh{
			Name:         meshName,
			Material:     mesh.Material,
			MaterialPath: mesh.MaterialPath,
			Indices:      idx,
			SingleBone:   -1,
		})
	}
	return boneBase
}
