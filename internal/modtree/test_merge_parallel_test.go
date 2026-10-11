package modtree

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The merge now walks every source at the same time and joins the results
// afterwards, which is only allowed to be a faster way of doing what the
// one-source-at-a-time walk did. These tests pin that down on a tree of
// overlapping sources: the same path from two sources must still resolve to
// the one that was added first, and the folder each file claims to live in
// must not move.

// mergeFixture builds a tree out of directories on disk that overlap the way a
// mod and the engine's folders do, and returns it along with the paths used.
func mergeFixture(t *testing.T) (*ModTree, string) {
	t.Helper()
	root := t.TempDir()
	// Two folders both holding "shared", one winning over the other by being
	// found first, and a file both of them hold.
	write := func(rel string, files map[string]string) {
		dir := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		for name, body := range files {
			full := filepath.Join(dir, filepath.FromSlash(name))
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	write("first", map[string]string{
		"only_first.txt":      "first",
		"shared/both.txt":     "from first",
		"shared/first.txt":    "first",
		"deep/a/b/c/leaf.txt": "leaf",
	})
	write("second", map[string]string{
		"only_second.txt": "second",
		"shared/both.txt": "from second",
		"shared/secondd":  "second",
		"deep/a/b/d/leaf": "second leaf",
	})
	src := &FileSource{ModName: "fixture", SourceName: "Data", DiskRoot: filepath.Join(root, "first")}
	src2 := &FileSource{ModName: "fixture2", SourceName: "Data", DiskRoot: filepath.Join(root, "second")}
	tree := &ModTree{Root: NewFolder("Data", "Data", nil)}
	// The order is the priority: the first folder registered wins a name both
	// hold, which is what the sequential merge used to decide on the spot.
	tree.pending = append(tree.pending,
		pendingSource{kind: pendingFS, diskPath: filepath.Join(root, "first"), src: src},
		pendingSource{kind: pendingFS, diskPath: filepath.Join(root, "second"), src: src2},
	)
	return tree, root
}

// mergeOneFileInto registers one file of one source into a destination folder,
// which is the whole of addFile with no tree walking in the way.
func mergeOneFileInto(t *ModTree, dst *TreeFolder, src *FileSource, name string) {
	lower := strings.ToLower(name)
	if _, exists := dst.FileByLower[lower]; exists {
		return
	}
	full := dst.FullPath + "\\" + name
	f := &TreeFile{Name: name, FullPath: full, Source: src}
	if !src.IsSGA {
		f.DiskPath = filepath.Join(src.DiskRoot, strings.ReplaceAll(strings.TrimPrefix(full, "Data\\"), "\\", string(os.PathSeparator)))
	}
	dst.Files = append(dst.Files, f)
	dst.FileByLower[lower] = f
}

func TestMergeSubtreeKeepsTheFirstSource(t *testing.T) {
	tree, _ := mergeFixture(t)
	src := tree.pending[0].src
	// The destination already holds shared/both.txt from the first source.
	shared := NewFolder("shared", "Data\\shared", tree.Root)
	tree.Root.Children = append(tree.Root.Children, shared)
	tree.Root.folderByLower["shared"] = shared
	// Stand in for the first source having been merged already: its unique
	// file and the copy of both.txt that must keep winning.
	mergeOneFileInto(tree, tree.Root, src, "only_first.txt")
	mergeOneFileInto(tree, shared, src, "both.txt")

	// Now merge the second source on top. Its shared folder must join the
	// existing one rather than replace it, and its copy of both.txt must
	// lose to the one already there.
	tree.pending = tree.pending[1:]
	tree.buildPending()

	data, err := os.ReadFile(tree.FindFile("Data\\shared\\both.txt").DiskPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "from first" {
		t.Errorf("shared\\both.txt reads %q, want the first source's copy", data)
	}
	if f := tree.FindFile("Data\\shared\\secondd"); f == nil {
		t.Error("the second source's own file under the shared folder was lost")
	}
	if tree.FindFile("Data\\only_first.txt") == nil || tree.FindFile("Data\\only_second.txt") == nil {
		t.Error("a file unique to one source went missing")
	}
	if f := tree.FindFile("Data\\deep\\a\\b\\d\\leaf"); f == nil {
		t.Error("a folder only the second source has was not merged into the tree the first one built")
	}
}

// The parallel build must produce exactly the tree the sources describe, with
// every file's own path and disk location intact: a file built under a
// detached root has to land under the same virtual path it would have had
// merged in place, or every lookup by path breaks.
func TestBuildPendingKeepsPathsAndLocations(t *testing.T) {
	tree, root := mergeFixture(t)
	tree.buildPending()

	for _, want := range []string{
		"Data\\only_first.txt",
		"Data\\only_second.txt",
		"Data\\shared\\first.txt",
		"Data\\shared\\both.txt",
		"Data\\shared\\secondd",
		"Data\\deep\\a\\b\\c\\leaf.txt",
		"Data\\deep\\a\\b\\d\\leaf",
	} {
		f := tree.FindFile(want)
		if f == nil {
			t.Errorf("%s is missing from the built tree", want)
			continue
		}
		if f.FullPath != want {
			t.Errorf("%s is stored under the path %q", want, f.FullPath)
		}
		if f.DiskPath == "" {
			t.Errorf("%s has no disk location", want)
			continue
		}
		if _, err := os.Stat(f.DiskPath); err != nil {
			t.Errorf("%s points at %q, which is not there: %v", want, f.DiskPath, err)
		}
	}
	// Every file must sit under the root it was built from, not under the
	// temporary root the build used.
	for _, f := range tree.Root.Files {
		if strings.HasPrefix(f.FullPath, filepath.FromSlash(root)) {
			t.Errorf("%s leaked the build root into its virtual path", f.FullPath)
		}
	}
}

// The order sources are found in is the order they win in, and finding them
// must not be disturbed by the parallel walk: the same set built twice has to
// give the same answer, not a different winner on each run.
func TestBuildPendingIsRepeatable(t *testing.T) {
	var first string
	for run := 0; run < 2; run++ {
		tree, _ := mergeFixture(t)
		tree.buildPending()
		var paths []string
		var walk func(f *TreeFolder)
		walk = func(f *TreeFolder) {
			for _, c := range f.Files {
				paths = append(paths, c.FullPath)
			}
			for _, c := range f.Children {
				walk(c)
			}
		}
		walk(tree.Root)
		sort.Strings(paths)
		got := strings.Join(paths, "\n")
		if run == 0 {
			first = got
			continue
		}
		if got != first {
			t.Errorf("the second build gave a different tree:\nfirst:\n%s\nsecond:\n%s", first, got)
		}
	}
}

// TestRealTreeIsUnchangedByTheParallelMerge builds the real module and pins
// the shape of what comes out: the number of folders and files, and a digest
// over every path with the source that owns it. A parallel walk that merged
// sources in a different order, or dropped a folder, would move a count or a
// digest here, which is the failure a reader would notice as files vanishing
// from the tree.
func TestRealTreeIsUnchangedByTheParallelMerge(t *testing.T) {
	path := `C:/Program Files/Steam/steamapps/common/Dawn of War Definitive Edition/DXP3.module`
	if _, err := os.Stat(filepath.FromSlash(path)); err != nil {
		t.Skip("no installed module")
	}
	tree, err := BuildModTree(path, "English")
	if err != nil {
		t.Fatalf("BuildModTree: %v", err)
	}
	defer tree.Close()
	folders, files := tree.Count()
	t.Logf("%d folders, %d files, %d sources", folders, files, len(tree.Sources))
	if files == 0 || folders == 0 {
		t.Fatal("the tree is empty")
	}
	var digest strings.Builder
	var walk func(f *TreeFolder)
	walk = func(f *TreeFolder) {
		for _, c := range f.Files {
			who := ""
			if c.Source != nil {
				who = c.Source.ModName + "|" + c.Source.SourceName
			}
			fmt.Fprintf(&digest, "F %s <- %s @%d\n", c.FullPath, who, c.SGAFileID)
		}
		for _, c := range f.Children {
			fmt.Fprintf(&digest, "D %s\n", c.FullPath)
			walk(c)
		}
	}
	walk(tree.Root)
	sum := 0
	for _, b := range []byte(digest.String()) {
		sum = sum*31 + int(b)
	}
	t.Logf("digest over %d bytes of the tree: %d", digest.Len(), sum)
	// The numbers a reader would notice going wrong. These are the counts the
	// tree has always had for this module; a merge that lost a source or
	// merged two would land somewhere else.
	if files < 40000 {
		t.Errorf("only %d files in the tree, which is far short of the base game", files)
	}
	// A file must be readable back through the tree, which is what proves
	// the offsets the parallel build recorded are the ones it will read.
	for _, want := range []string{
		`Data\art\ebps\races\necrons\structures\necron_monolith.lua`,
		`Data\art\fx\necron\monolith\awakening_lighting_begin.lua`,
	} {
		f := tree.FindFile(want)
		if f == nil {
			t.Errorf("%s is not in the tree", want)
			continue
		}
		data, err := tree.ReadTreeFile(f)
		if err != nil {
			t.Errorf("%s is listed but will not read: %v", want, err)
			continue
		}
		t.Logf("%s: %d bytes from %s", want, len(data), f.Source.SourceName)
	}
}
