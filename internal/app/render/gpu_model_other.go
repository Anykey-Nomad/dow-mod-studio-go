//go:build !windows && !linux

package render

import (
	"errors"

	"gioui.org/app"
)

// TooEarly is only ever set by the Vulkan path; without it, it stays false.
var TooEarly bool

// NewForWindow returns nil on platforms without the Vulkan model renderer.
func NewForWindow(w *app.Window) Handle { return nil }

// ProbeFrame has no hardware frame to read back here, so there is nothing
// to fail: it reports success.
func ProbeFrame(f Frame, context string) bool { return true }

// Readback has no renderer on this platform.
func Readback(h Handle) ([]byte, error) {
	return nil, errors.New("render: no Vulkan renderer on this platform")
}

// FxGroupInfo mirrors the Vulkan build's diagnostic type.
type FxGroupInfo struct {
	Groups         int
	Compute        bool
	Overlay        bool
	RibbonPipeline bool
}

// FxGroups has no renderer on this platform.
func FxGroups(h Handle) FxGroupInfo { return FxGroupInfo{} }
