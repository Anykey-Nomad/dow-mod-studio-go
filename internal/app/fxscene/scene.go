// Package fxscene holds the data one frame's FX overlay is drawn from: the
// particles and ribbons grouped by the map they read, and the frame-wide
// light they are shaded with. It sits below the overlay that builds the scene
// and the renderers that draw it, so both sides agree on plain data instead
// of reaching into each other's package.
package fxscene

import (
	"image"

	"dow-mod-studio/internal/fx"

	"github.com/fogleman/fauxgl"
)

// Scene is one frame's particles, grouped by the map they read: a draw
// covers every particle sharing one texture. It is built on the CPU, where
// the simulation runs, and drawn by whichever renderer has the frame.
type Scene struct {
	Groups []Group
	// Textures is the decoded map of each group, keyed as the groups are:
	// a renderer that draws the scene uploads what it finds here.
	Textures map[string]*image.RGBA
}

// Group is one draw: every sprite and ribbon sharing a texture, a blend mode
// and an effect site.
type Group struct {
	Texture  string
	Additive bool
	// Origin is where the effect plays: every sprite of the group is
	// placed at its own offset from it.
	Origin  [3]float64
	Sprites []fx.Sprite
	// Beams are the ribbons this group draws. Their corners are already in
	// model space, so Origin does not apply to them: a beam is stretched
	// between two markers rather than placed at one.
	Beams []Beam
}

// Beam is one ribbon and the eye it is turned towards. The tessellation
// depends on the camera, so the eye travels with the ribbon.
type Beam struct {
	Ribbon fx.BeamRibbon
	Eye    fauxgl.Vector
}

// Light is the frame-wide light sent to the FX vertex shader, so sprites and
// ribbons share one lighting setup rather than CPU shading every particle
// separately.
type Light struct {
	Dir     fauxgl.Vector // towards the light, in model space
	Ambient float64
	Diffuse float64
}
