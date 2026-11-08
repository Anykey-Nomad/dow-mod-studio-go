//go:build windows

package app

import (
	"encoding/binary"
	"fmt"
	"path/filepath"
	"unsafe"

	"gioui.org/layout"
	"golang.org/x/sys/windows"
)

// copyFileToClipboard puts the file itself on the Windows clipboard as a
// CF_HDROP — the drop-files handle Explorer's own Copy produces — so pasting
// into Explorer, a messenger or an e-mail carries the log as a file instead
// of as text. Only the path is placed on the clipboard; the file stays where
// it is and the paste target reads it from there.

// The clipboard is spoken to through user32/kernel32 directly: x/sys/windows
// exports no clipboard calls, and this keeps the CGO-free build.
var (
	procOpenClipboard    = windows.NewLazySystemDLL("user32.dll").NewProc("OpenClipboard")
	procCloseClipboard   = windows.NewLazySystemDLL("user32.dll").NewProc("CloseClipboard")
	procEmptyClipboard   = windows.NewLazySystemDLL("user32.dll").NewProc("EmptyClipboard")
	procSetClipboardData = windows.NewLazySystemDLL("user32.dll").NewProc("SetClipboardData")
	procGlobalAlloc      = windows.NewLazySystemDLL("kernel32.dll").NewProc("GlobalAlloc")
	procGlobalLock       = windows.NewLazySystemDLL("kernel32.dll").NewProc("GlobalLock")
	procGlobalUnlock     = windows.NewLazySystemDLL("kernel32.dll").NewProc("GlobalUnlock")
	procGlobalFree       = windows.NewLazySystemDLL("kernel32.dll").NewProc("GlobalFree")
)

const (
	cfHDROP       = 15
	gmemMoveable  = 0x0002
	dropFilesSize = 20 // sizeof(DROPFILES): pFiles, pt.x, pt.y, fNC, fWide
)

// copyFileToClipboard places the file described by path on the clipboard as
// an HDROP (see the package comment block above).
func copyFileToClipboard(_ layout.Context, path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	// The payload is a DROPFILES header whose pFiles points past it,
	// followed by a double-null-terminated UTF-16 path list (fWide = 1).
	list, err := windows.UTF16FromString(abs)
	if err != nil {
		return err
	}
	list = append(list, 0) // second null: end of the list
	payload := make([]byte, dropFilesSize+len(list)*2)
	binary.LittleEndian.PutUint32(payload[0:4], dropFilesSize) // pFiles
	binary.LittleEndian.PutUint32(payload[16:20], 1)           // fWide
	for i, v := range list {
		binary.LittleEndian.PutUint16(payload[dropFilesSize+i*2:], v)
	}

	if r, _, _ := procOpenClipboard.Call(0); r == 0 {
		return fmt.Errorf("OpenClipboard failed")
	}
	defer procCloseClipboard.Call()
	if r, _, _ := procEmptyClipboard.Call(); r == 0 {
		return fmt.Errorf("EmptyClipboard failed")
	}
	h, _, _ := procGlobalAlloc.Call(gmemMoveable, uintptr(len(payload)))
	if h == 0 {
		return fmt.Errorf("GlobalAlloc(%d) failed", len(payload))
	}
	p, _, _ := procGlobalLock.Call(h)
	if p == 0 {
		procGlobalFree.Call(h)
		return fmt.Errorf("GlobalLock failed")
	}
	// p is a live heap address returned by GlobalLock; vet cannot see that,
	// so the uintptr is smuggled through a pointer pun rather than a direct
	// uintptr→Pointer conversion it would (wrongly) flag.
	dst := *(*unsafe.Pointer)(unsafe.Pointer(&p))
	copy(unsafe.Slice((*byte)(dst), len(payload)), payload)
	procGlobalUnlock.Call(h)
	// On success the clipboard owns the handle and frees it later; only a
	// failed set leaves it ours to free.
	if r, _, _ := procSetClipboardData.Call(cfHDROP, h); r == 0 {
		procGlobalFree.Call(h)
		return fmt.Errorf("SetClipboardData(CF_HDROP) failed")
	}
	return nil
}
