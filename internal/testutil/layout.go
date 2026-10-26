// Package testutil holds the fixtures the application's tests and the
// renderer's tests built for themselves separately before the code was split
// into packages: layout contexts, game-install lookups and render options.
// Living here keeps one copy of each instead of one per package.
package testutil

import (
	"image"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
)

// NewLayoutContext builds a fixed-size Gio layout context, which is what a
// paint or layout test draws into without opening a window.
func NewLayoutContext(ops *op.Ops, w, h int) layout.Context {
	return layout.Context{
		Ops:         ops,
		Constraints: layout.Exact(image.Pt(w, h)),
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
	}
}
