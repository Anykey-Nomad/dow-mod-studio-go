package modtree

import (
	"os"
	"path/filepath"
	"testing"
)

// HasFile answers "is there a file here" for the callers that walk a model's or
// an effect's file list. It has to agree with reading the file would — a name
// it calls missing is a name nothing will ever try to load — but it has to get
// there without reading, which is the whole reason it exists.
func TestHasFileAgreesWithReadingWithoutReading(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) string {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	filled := write("Data/filled.txt", "contents")
	write("Data/empty.txt", "")
	if err := os.MkdirAll(filepath.Join(root, "Data", "folder.txt"), 0o755); err != nil {
		t.Fatal(err)
	}

	src := &FileSource{ModName: "fixture", SourceName: "Data", DiskRoot: filepath.Join(root, "Data")}
	tree := &ModTree{Root: NewFolder("Data", "Data", nil)}
	srcFolder := NewFolder("fixture", "Data", tree.Root)
	tree.Root.Children = append(tree.Root.Children, srcFolder)
	tree.Root.folderByLower["fixture"] = srcFolder
	tree.Sources = append(tree.Sources, src)
	tree.addFile(srcFolder, "filled.txt", src)
	tree.addFile(srcFolder, "empty.txt", src)
	tree.addFile(srcFolder, "folder.txt", src)

	for _, tc := range []struct {
		name string
		want bool
	}{
		{"Data\\fixture\\filled.txt", true},
		{"Data\\fixture\\empty.txt", false},
		{"Data\\fixture\\folder.txt", false},
		{"Data\\fixture\\missing.txt", false},
		{"Data\\filled.txt", false},
	} {
		if got := tree.HasFile(tc.name); got != tc.want {
			t.Errorf("HasFile(%s) is %v, want %v", tc.name, got, tc.want)
		}
	}
	if tree.HasTreeFile(nil) {
		t.Error("HasTreeFile(nil) reported a file")
	}
	// A name it calls present has to be readable, or it has answered wrongly.
	if data, err := tree.ReadFile("Data\\fixture\\filled.txt"); err != nil {
		t.Errorf("the file HasFile found will not read: %v", err)
	} else if string(data) != "contents" {
		t.Errorf("it reads %q", data)
	}
	// A file taken away under the tree stops being there, which is what reading
	// it used to say too: the answer has to agree with the old one in the cases
	// that matter, or a name that used to be skipped now looks loadable.
	if err := os.Remove(filled); err != nil {
		t.Fatal(err)
	}
	if tree.HasFile("Data\fixture\filled.txt") {
		t.Error("a file that is no longer on disk is still reported as there")
	}
}
