//go:build !windows

package modifiers

import "gioui.org/io/key"

// heldModifiers is the non-Windows stub: without a live key-state reader the
// modifier state comes only from the key events the tracker receives, so the
// tracked flags are all there is on this platform.
func HeldModifiers() key.Modifiers {
	return 0
}
