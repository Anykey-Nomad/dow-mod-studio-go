//go:build !windows

package main

import "gioui.org/app"

// newModelGpuForWindow returns nil off Windows: the hardware renderer draws
// through Gio's Direct3D 11 backend, and the 3D view falls back to the
// software renderer everywhere else.
func newModelGpuForWindow(w *app.Window) modelGpuHandle { return nil }
