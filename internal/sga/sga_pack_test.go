package sga_test

import (
	"dow-mod-studio/internal/sga"
	"dow-mod-studio/internal/sgapack"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteSGA2RoundTrip(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.txt")
	second := filepath.Join(dir, "second.lua")
	nestedDir := filepath.Join(dir, "nested", "deep")
	if err := os.MkdirAll(nestedDir, 0755); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(nestedDir, "nested.txt")
	if err := os.WriteFile(first, []byte("first file"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("return true"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(nested, []byte("nested file"), 0644); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(dir, "test.sga")
	if err := sgapack.WriteSGA2(archive, map[string]string{
		`Data\first.txt`:              first,
		`Data\second.lua`:             second,
		`Data\nested\deep\nested.txt`: nested,
	}, "Data"); err != nil {
		t.Fatal(err)
	}
	sga, err := sga.OpenSGA(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer sga.Close()
	if sga.ToCCount() != 1 || !strings.EqualFold(sga.ToCName(0), "Data") {
		t.Fatalf("unexpected ToC: count=%d name=%q", sga.ToCCount(), sga.ToCName(0))
	}
	root, err := sga.RootDir(0)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := sga.List(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 root entries, got %d", len(entries))
	}
	var foundNested bool
	for _, entry := range entries {
		if entry.IsDir {
			if entry.Name == "nested" {
				children, err := sga.List(entry.DirID)
				if err != nil {
					t.Fatal(err)
				}
				if len(children) != 1 || children[0].Name != "deep" {
					t.Fatalf("unexpected nested directory: %#v", children)
				}
				deepFiles, err := sga.List(children[0].DirID)
				if err != nil {
					t.Fatal(err)
				}
				if len(deepFiles) != 1 {
					t.Fatalf("expected one nested file, got %d", len(deepFiles))
				}
				data, err := sga.ReadFile(deepFiles[0].FileID)
				if err != nil {
					t.Fatal(err)
				}
				if string(data) != "nested file" {
					t.Fatalf("unexpected nested data %q", data)
				}
				foundNested = true
			}
			continue
		}
		data, err := sga.ReadFile(entry.FileID)
		if err != nil {
			t.Fatal(err)
		}
		if len(data) == 0 {
			t.Fatalf("empty packed file %s", entry.Name)
		}
	}
	if !foundNested {
		t.Fatal("nested entry not found")
	}
}
