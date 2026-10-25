//go:build !windows

package main

import (
	"os"
	"path/filepath"
)

// File associations are a Windows feature here: the registry machinery lives
// in assoc_windows.go, and these stubs keep the shared callers compiling. The
// settings dialog hides its association section when supported() is false.

func associationsSupported() bool { return false }

func isExtAssociated(t assocType) (bool, string, error) { return false, "", nil }
func registerExtAssociation(t assocType) error          { return nil }
func unregisterExtAssociation(t assocType) error        { return nil }

// getExePath returns the absolute path of the current executable.
func getExePath() string {
	p, err := os.Executable()
	if err != nil {
		return ""
	}
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	if eval, err := filepath.EvalSymlinks(p); err == nil {
		p = eval
	}
	return p
}
