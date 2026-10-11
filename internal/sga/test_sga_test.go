package sga

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// realArchive is the shipped archive the header parse is worth testing
// against: a table this size is what the cache exists for. The tests that
// need it skip themselves when the game is not installed.
const realArchive = `C:/Program Files/Steam/steamapps/common/Dawn of War Definitive Edition/DoWDE/DoWDE.sga`

// openReal opens the shipped archive, skipping the test when it is not there.
func openReal(t *testing.T) *SGAFile {
	t.Helper()
	f, err := OpenSGA(filepath.FromSlash(realArchive))
	if err != nil {
		t.Skipf("no installed archive: %v", err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

// writeTemp makes an empty file and returns its path.
func writeTemp(t *testing.T, dir, name string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(name), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func statOf(t *testing.T, path string) (int64, int64) {
	t.Helper()
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return st.Size(), st.ModTime().UnixNano()
}

// Opening an archive a second time must not parse its header again: that parse
// is the part of opening the file that is not the operating system's, and the
// mod tree is rebuilt on every refresh, so the same archives are opened over
// and over. Two handles that share one index is what "did not parse again"
// looks like from the outside.
func TestOpenSGAReusesTheParsedTable(t *testing.T) {
	first := openReal(t)
	second, err := OpenSGA(filepath.FromSlash(realArchive))
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if first.index != second.index {
		t.Fatal("the second open parsed the header again instead of using the cached table")
	}
	// A shared table is only safe if both handles still answer for it.
	if first.Version() != second.Version() || first.ToCCount() != second.ToCCount() {
		t.Error("the two handles disagree about the archive they share")
	}
	root, err := first.RootDir(0)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := first.List(root)
	if err != nil {
		t.Fatal(err)
	}
	other, err := second.List(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != len(other) || len(entries) == 0 {
		t.Fatalf("the two handles list %d and %d entries", len(entries), len(other))
	}
	t.Logf("%d ToC entries, %d files under the first entry point", first.ToCCount(), len(entries))
}

// The cache is keyed by where the archive is and by what it last looked like,
// so a table parsed from a file that has since changed must not be handed out
// for the new one, and an archive that has not changed must be.
func TestCacheIsDroppedWhenTheFileChanges(t *testing.T) {
	dir := t.TempDir()
	path := writeTemp(t, dir, "changed.sga")
	size, modTime := statOf(t, path)
	rememberIndex(path, &index{version: 9}, size, modTime)

	if got := cachedIndex(path); got == nil || got.version != 9 {
		t.Fatal("an unchanged archive did not come back from the cache")
	}
	// A file of the same name but a different size is a different archive, so
	// the old table must not be handed to whoever opens it next.
	if err := os.WriteFile(path, []byte("changed.sga and more"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := cachedIndex(path); got != nil {
		t.Errorf("a changed archive came back with the table of the old one (version %d)", got.version)
	}
	// Reparsing it stores the new table in its place.
	idx := &index{version: 10}
	size, modTime = statOf(t, path)
	rememberIndex(path, idx, size, modTime)
	if got := cachedIndex(path); got != idx {
		t.Error("the newer table did not replace the older one")
	}
}

// A session that visits many archives must not keep every header it ever saw,
// and the one it drops has to be the one it saw longest ago rather than an
// arbitrary one.
func TestCacheEvictsTheOldestArchive(t *testing.T) {
	dir := t.TempDir()
	paths := make([]string, cacheLimit+5)
	for i := range paths {
		paths[i] = writeTemp(t, dir, "arch"+string(rune('a'+i%26))+string(rune('0'+i/26))+".sga")
		size, modTime := statOf(t, paths[i])
		rememberIndex(paths[i], &index{version: uint32(i + 1)}, size, modTime)
	}
	for i, path := range paths[cacheLimit:] {
		if got := cachedIndex(path); got == nil {
			t.Errorf("%s was evicted, but it is one of the newest", path)
		} else if got.version != uint32(i+cacheLimit+1) {
			t.Errorf("%s came back as version %d, want %d", path, got.version, i+cacheLimit+1)
		}
	}
	for _, path := range paths[:5] {
		if got := cachedIndex(path); got != nil {
			t.Errorf("%s is still cached, but it is the oldest of the lot", path)
		}
	}
}

// Reading a batch has to answer exactly as reading the same files one by one
// does, in the order asked for, with a failure beside its own entry instead of
// taking the whole batch down. That is what lets a caller fetch a model's
// textures without checking every name by hand first.
func TestReadFilesMatchesReadingOneByOne(t *testing.T) {
	f := openReal(t)
	root, err := f.RootDir(0)
	if err != nil {
		t.Fatal(err)
	}
	var ids []int
	var walk func(dirID int, depth int)
	walk = func(dirID int, depth int) {
		if depth > 3 || len(ids) >= 24 {
			return
		}
		entries, err := f.List(dirID)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if len(ids) >= 24 {
				return
			}
			if e.IsDir {
				walk(e.DirID, depth+1)
				continue
			}
			ids = append(ids, e.FileID)
		}
	}
	walk(root, 0)
	if len(ids) < 8 {
		t.Skipf("only %d files found to read", len(ids))
	}
	// An id no archive has, in the middle of the batch, stands in for a name
	// that resolved to nothing: it must not shift the answers around it.
	want := make([][]byte, 0, len(ids)+1)
	ids = append(ids[:len(ids)/2], append([]int{-1}, ids[len(ids)/2:]...)...)

	batch, errs := f.ReadFiles(ids)
	if len(batch) != len(ids) || len(errs) != len(ids) {
		t.Fatalf("asked for %d files, got %d results and %d errors", len(ids), len(batch), len(errs))
	}
	for i, id := range ids {
		one, _ := f.ReadFile(id)
		if id < 0 {
			if errs[i] == nil {
				t.Errorf("entry %d is a file id no archive has, but it reported no error", i)
			}
			if batch[i] != nil {
				t.Errorf("entry %d reported both bytes and an error", i)
			}
			continue
		}
		if errs[i] != nil {
			t.Errorf("entry %d (id %d) failed in the batch: %v", i, id, errs[i])
			continue
		}
		if !bytes.Equal(batch[i], one) {
			t.Errorf("entry %d (id %d) read %d bytes in the batch and %d on its own", i, id, len(batch[i]), len(one))
		}
		want = append(want, one)
	}
	t.Logf("read %d files in a batch, %d bytes", len(ids), totalBytes(batch))
}

// An empty batch is a question with no files in it, not a failure.
func TestReadFilesWithNothingToRead(t *testing.T) {
	f := openReal(t)
	for _, ids := range [][]int{nil, {}} {
		batch, errs := f.ReadFiles(ids)
		if len(batch) != 0 || len(errs) != 0 {
			t.Errorf("reading %d ids gave %d results and %d errors", len(ids), len(batch), len(errs))
		}
	}
	if _, err := f.ReadFile(0x7fffffff); err == nil {
		t.Error("an id past the end of the file table was accepted")
	}
}

func totalBytes(b [][]byte) int {
	n := 0
	for _, x := range b {
		n += len(x)
	}
	return n
}
