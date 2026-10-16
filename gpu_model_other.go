//go:build !windows && !linux

package main

import "gioui.org/app"

// modelGpuTooEarly is only ever set by the Vulkan path; without it, it stays false.
var modelGpuTooEarly bool

// newModelGpuForWindow returns nil on platforms without the Vulkan model renderer.
func newModelGpuForWindow(w *app.Window) modelGpuHandle { return nil }

// probeGpuFrame has no hardware frame to read back here, so there is nothing
// to fail: it reports success.
func probeGpuFrame(f modelGpuFrame, context string) bool { return true }
