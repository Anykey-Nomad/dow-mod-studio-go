package main

import (
	"gioui.org/font/gofont"
	"gioui.org/text"
	"gioui.org/widget/material"
)

// unitTestTheme builds a material theme with a real font collection, without
// which every label in the tab would panic on a nil shaper.
func unitTestTheme() *material.Theme {
	th := material.NewTheme()
	th.Shaper = text.NewShaper(text.WithCollection(gofont.Collection()))
	return th
}
