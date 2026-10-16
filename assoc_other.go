//go:build !windows

package main

import (
	"os"
	"path/filepath"
)

func isModuleAssociated() (bool, string, error) { return false, "", nil }
func registerModuleAssociation() error          { return nil }
func unregisterModuleAssociation() error        { return nil }

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
