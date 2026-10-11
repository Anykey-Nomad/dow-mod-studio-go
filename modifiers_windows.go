//go:build windows

package main

import (
	"gioui.org/io/key"
	"golang.org/x/sys/windows"
)

// user32.GetKeyState is read directly instead of going through the Modifiers
// on Gio's key and pointer events: the window backend's getModifiers masks
// the key state with 0x1000, a bit Windows never sets for a key (down is the
// high bit, 0x8000), so every event it builds carries no modifiers at all.
// Reading the key state here has no such layer in between: if the user is
// holding Ctrl while the mouse goes down, this says so, which is all a
// Ctrl+click needs. x/sys/windows exposes no GetKeyState, hence the proc.
var (
	user32          = windows.NewLazySystemDLL("user32.dll")
	procGetKeyState = user32.NewProc("GetKeyState")
)

// winKeyState reports whether the virtual key is down right now.
func winKeyState(vk uintptr) bool {
	c, _, _ := procGetKeyState.Call(vk)
	// GetKeyState returns a SHORT: the high bit of the low 16 bits means down.
	return int16(c&0xffff) < 0
}

// heldModifiers reads the live state of the modifier keys straight from the OS.
func heldModifiers() key.Modifiers {
	var m key.Modifiers
	if winKeyState(windows.VK_CONTROL) {
		m |= key.ModCtrl
	}
	if winKeyState(windows.VK_SHIFT) {
		m |= key.ModShift
	}
	if winKeyState(windows.VK_MENU) {
		m |= key.ModAlt
	}
	if winKeyState(windows.VK_LWIN) || winKeyState(windows.VK_RWIN) {
		m |= key.ModSuper
	}
	return m
}
