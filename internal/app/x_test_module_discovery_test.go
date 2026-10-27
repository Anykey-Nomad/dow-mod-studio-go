package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverModuleFiles(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"UA.module", "DXP3.MODULE", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("test"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "Nested.module"), 0700); err != nil {
		t.Fatal(err)
	}

	got := discoverModuleFiles([]string{dir, dir})
	if len(got) != 2 {
		t.Fatalf("expected two module files, got %d: %#v", len(got), got)
	}
	if got[0].Path != filepath.Join(dir, "DXP3.MODULE") || got[1].Path != filepath.Join(dir, "UA.module") {
		t.Fatalf("unexpected module order: %#v", got)
	}
}
