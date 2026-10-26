//go:build !windows

package assoc

import (
	"os"
	"path/filepath"
)

// File associations are a Windows feature here: the registry machinery lives
// in assoc_windows.go, and these stubs keep the shared callers compiling. The
// settings dialog hides its association section when supported() is false.

func AssociationsSupported() bool { return false }

func IsExtAssociated(t AssocType) (bool, string, error) { return false, "", nil }
func RegisterExtAssociation(t AssocType) error          { return nil }
func UnregisterExtAssociation(t AssocType) error        { return nil }

// getExePath returns the absolute path of the current executable.
func GetExePath() string {
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
