package whm

import "strings"

// MeshAlpha is how much of a mesh the clip named draws at moment t, 0..1 with
// t normalised over the clip as the other keyframe times are.
//
// A track with keys is read as the curve it is; a track with none is read off
// its force flag, which is what says a mesh is hidden outright for the whole
// clip. Before a curve's first key the flag still applies, which is how a mesh
// held back until a build animation lets it out reads as hidden at the start
// and solid from the moment the curve begins.
//
// A clip with no track for the name at all draws it fully: the tracks are
// written for the meshes a clip hides, and everything else is left alone.
func (a *Animation) MeshAlpha(name string, t float32) float32 {
	if a == nil {
		return 1
	}
	for i := range a.Meshes {
		if !strings.EqualFold(a.Meshes[i].Name, name) {
			continue
		}
		return meshTrackAlpha(&a.Meshes[i], t)
	}
	return 1
}

// meshTrackAlpha is one visibility track read at a moment.
//
// A track with no curve is not a track at all: the flag is only the value a
// mesh is held at while a curve has not reached it yet, so with nothing to
// interpolate the mesh is simply drawn whole. That is what the shipped
// models say - the restored monolith's idle, aim, fire and death clips carry
// a flag of zero on all twenty-three of their meshes and every one of them is
// plainly on screen the whole clip.
//
// A track with a curve holds the mesh at its flag until the first key, which
// is how phase_in keeps a model back for the frame before it lets it out and
// how phase_out lets one stand and then takes it away.
func meshTrackAlpha(tr *MeshTrack, t float32) float32 {
	keys := tr.Keys
	if len(keys) == 0 {
		return 1
	}
	if t <= keys[0].Time {
		if tr.ForceInvisible {
			return 0
		}
		return 1
	}
	if t >= keys[len(keys)-1].Time {
		return clampMeshAlpha(keys[len(keys)-1].V[0])
	}
	for i := 1; i < len(keys); i++ {
		prev, next := keys[i-1], keys[i]
		if t > next.Time {
			continue
		}
		span := next.Time - prev.Time
		if span <= 0 {
			return clampMeshAlpha(next.V[0])
		}
		f := (t - prev.Time) / span
		v := prev.V[0] + (next.V[0]-prev.V[0])*f
		return clampMeshAlpha(v)
	}
	return 1
}

// clampMeshAlpha keeps a track's value inside 0..1. The keys are written as
// alphas, but a curve is free to overshoot and a value outside the range is
// not something a renderer can be asked to draw.
func clampMeshAlpha(v float32) float32 {
	if v < 0 || v > 1 {
		return 0
	}
	return v
}

// AlphaByte is an alpha as the byte a tint carries. It is rounded to
// nearest rather than truncated, which is what keeps a fully faded mesh fully
// invisible and a whole one exactly whole: a hardware premultiply of 254/255
// leaves a mesh one step short of opaque, and a truncated 0.999 is 0.9978,
// which shows up as a mesh that never quite reaches full colour.
func AlphaByte(alpha float32) uint8 {
	if alpha <= 0 {
		return 0
	}
	if alpha >= 1 {
		return 255
	}
	return uint8(float64(alpha)*255 + 0.5)
}

// MeshAlphas is what every mesh of the named clip draws at moment t, keyed by
// mesh name, and nil when the clip draws all of them.
//
// A nil map is the common case and is what lets a renderer skip the work
// entirely: only the clips that hide or fade something carry one, and most
// models have none of those in most of their clips.
func (m *Model) MeshAlphas(anim string, t float32) map[string]float32 {
	if m == nil {
		return nil
	}
	clip := m.Animation(anim)
	if clip == nil || len(clip.Meshes) == 0 {
		return nil
	}
	var out map[string]float32
	for i := range clip.Meshes {
		tr := &clip.Meshes[i]
		a := meshTrackAlpha(tr, t)
		if a >= 1 {
			continue
		}
		if out == nil {
			out = map[string]float32{}
		}
		out[tr.Name] = a
	}
	return out
}

// Animation is the named clip, matched without regard to case because the
// timelines name clips in lower case and the model lists them as the artist
// typed them.
func (m *Model) Animation(name string) *Animation {
	for i := range m.Animations {
		if strings.EqualFold(m.Animations[i].Name, name) {
			return &m.Animations[i]
		}
	}
	return nil
}
