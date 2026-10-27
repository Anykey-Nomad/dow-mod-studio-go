package fileops_test

import (
	"dow-mod-studio/internal/app/fileops"
	"dow-mod-studio/internal/app/shell"
	"dow-mod-studio/internal/modtree"
	"os"
	"path/filepath"
	"testing"
)

func TestExternalTextFileExtensions(t *testing.T) {
	ui := &shell.UiState{}
	for _, path := range []string{"script.lua", "readme.txt", "unit.ai", "battle.scar", "voice.rat", "console.con"} {
		if !fileops.IsExternalTextFile(ui, path) {
			t.Errorf("isExternalTextFile(%q) = false, want true", path)
		}
	}
	for _, path := range []string{"data.rgd", "sound.fda", "texture.dds", "module.module"} {
		if fileops.IsExternalTextFile(ui, path) {
			t.Errorf("isExternalTextFile(%q) = true, want false", path)
		}
	}
}

// TestTextFormatsAreParsedAsTyped covers the separators a person actually
// uses. A list written with commas, with spaces, or with both has to come out
// the same, and a leading dot has to be optional in every case.
func TestTextFormatsAreParsedAsTyped(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{".lua, .txt, .ai", []string{".lua", ".txt", ".ai"}},
		{".lua .txt .ai", []string{".lua", ".txt", ".ai"}},
		{".lua,.txt,.ai", []string{".lua", ".txt", ".ai"}},
		{".lua,  .txt   .ai", []string{".lua", ".txt", ".ai"}},
		{"lua, txt, ai", []string{".lua", ".txt", ".ai"}},
		{".LUA, .Txt", []string{".lua", ".txt"}},
		{".lua, .txt, .lua", []string{".lua", ".txt"}},
		{".lua,.custom.dls", []string{".lua", ".custom.dls"}},
	}
	for _, c := range cases {
		got := fileops.ParseTextFormats(c.in)
		if len(got) != len(c.want) {
			t.Errorf("parseTextFormats(%q) = %v, want %v", c.in, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("parseTextFormats(%q) = %v, want %v", c.in, got, c.want)
				break
			}
		}
	}
}

// An empty field must not lock the reader out of every file, so clearing it
// falls back to the built-in list rather than to nothing.
func TestEmptyTextFormatListFallsBackToTheDefault(t *testing.T) {
	for _, in := range []string{"", "   ", ",,,"} {
		got := fileops.ParseTextFormats(in)
		if len(got) != len(fileops.DefaultTextFormats) {
			t.Errorf("parseTextFormats(%q) returned %d formats, want the %d defaults", in, len(got), len(fileops.DefaultTextFormats))
		}
	}
}

// The whole point of the setting: a format nobody shipped with is opened by
// one click, and one the reader took back out is not.
func TestAReaderCanExtendTheTextEditorFormats(t *testing.T) {
	ui := &shell.UiState{TextEditorFormats: fileops.ParseTextFormats(".lua, .txt, .dsl")}
	for _, path := range []string{"a.lua", "a.txt", "a.dsl", "nested/path/to/a.dsl", "a.DSL"} {
		if !fileops.IsEditorFile(ui, path) {
			t.Errorf("isEditorFile(%q) = false, want true", path)
		}
	}
	for _, path := range []string{"a.ai", "a.con", "a.rgd"} {
		if fileops.IsEditorFile(ui, path) {
			t.Errorf("isEditorFile(%q) = true, want false once it was left out", path)
		}
	}
	// One click, not two: the same list decides the double click.
	if !fileops.IsExternalTextFile(ui, "a.dsl") {
		t.Error("isExternalTextFile did not follow the reader's list")
	}
}

// What the field shows has to be what it reads back, or reopening the dialog
// loses what was typed.
func TestTextFormatsRoundTripThroughTheFieldText(t *testing.T) {
	for _, in := range []string{".lua, .txt, .dsl", ".LUA .TXT", ".lua,.txt"} {
		text := fileops.FormatTextFormats(fileops.ParseTextFormats(in))
		back := fileops.ParseTextFormats(text)
		want := fileops.ParseTextFormats(in)
		if len(back) != len(want) {
			t.Errorf("round trip of %q gave %q -> %v, want %v", in, text, back, want)
			continue
		}
		for i := range back {
			if back[i] != want[i] {
				t.Errorf("round trip of %q gave %q -> %v, want %v", in, text, back, want)
				break
			}
		}
	}
}

// A file with no extension at all is never text, whatever the list says.
func TestAFileWithNoExtensionIsNotText(t *testing.T) {
	ui := &shell.UiState{TextEditorFormats: fileops.ParseTextFormats(".lua")}
	for _, path := range []string{"Makefile", "dir/file", ""} {
		if fileops.IsEditorFile(ui, path) {
			t.Errorf("isEditorFile(%q) = true, want false", path)
		}
	}
}

func TestMoveFileToRecycleAndClear(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "mod", "Data", "example.txt")
	if err := os.MkdirAll(filepath.Dir(source), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("payload"), 0644); err != nil {
		t.Fatal(err)
	}
	recycle := filepath.Join(root, "recycle")
	destination, err := fileops.MoveFileToRecycle(source, recycle)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(source); !os.IsNotExist(err) {
		t.Fatalf("source still exists, stat error=%v", err)
	}
	if data, err := os.ReadFile(destination); err != nil || string(data) != "payload" {
		t.Fatalf("recycled data = %q, error=%v", data, err)
	}
	if got := fileops.CountFilesInDir(recycle); got != 1 {
		t.Fatalf("recycle file count = %d, want 1", got)
	}
	if err := fileops.ClearRecycleDir(recycle); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(recycle)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("recycle contains %d entries after clear", len(entries))
	}
}

func TestDuplicateVirtualPathAvoidsExistingName(t *testing.T) {
	tree := &modtree.ModTree{Root: modtree.NewFolder("Data", "Data", nil)}
	folder := tree.GetOrCreateFolder(tree.Root, "sound", &modtree.FileSource{Kind: modtree.KindThisMod})
	file := &modtree.TreeFile{Name: "track.wav", FullPath: `Data\sound\track.wav`, Source: folderSourceForTest(), DiskPath: "track.wav"}
	folder.Files = append(folder.Files, file)
	folder.FileByLower[file.Name] = file

	got := fileops.DuplicateVirtualPath(tree, `Data\sound\track.wav`)
	want := `Data\sound\track (1).wav`
	if got != want {
		t.Fatalf("duplicateVirtualPath() = %q, want %q", got, want)
	}
}

func TestCanDeleteCurrentModFile(t *testing.T) {
	appPath := t.TempDir()
	modRoot := filepath.Join(appPath, "TestMod", "Data")
	if err := os.MkdirAll(modRoot, 0755); err != nil {
		t.Fatal(err)
	}
	currentPath := filepath.Join(modRoot, "current.txt")
	requiredPath := filepath.Join(appPath, "RequiredMod", "Data", "required.txt")
	for _, path := range []string{currentPath, requiredPath} {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("payload"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	tree := &modtree.ModTree{Root: modtree.NewFolder("Data", "Data", nil), AppPath: appPath, FileMapName: "TestMod"}
	folder := tree.GetOrCreateFolder(tree.Root, "files", &modtree.FileSource{Kind: modtree.KindThisMod})
	current := &modtree.TreeFile{Name: "current.txt", FullPath: `Data\files\current.txt`, Source: &modtree.FileSource{Kind: modtree.KindThisMod, DiskRoot: modRoot}, DiskPath: currentPath}
	required := &modtree.TreeFile{Name: "required.txt", FullPath: `Data\files\required.txt`, Source: &modtree.FileSource{Kind: modtree.KindReqMod, DiskRoot: filepath.Dir(requiredPath)}, DiskPath: requiredPath}
	sga := &modtree.TreeFile{Name: "archive.txt", FullPath: `Data\files\archive.txt`, Source: &modtree.FileSource{Kind: modtree.KindThisMod, IsSGA: true}}
	folder.Files = append(folder.Files, current, required, sga)
	for _, file := range folder.Files {
		folder.FileByLower[file.Name] = file
	}
	ui := &shell.UiState{ModTree: tree}
	if !fileops.CanDeleteCurrentModFile(ui, current.FullPath) {
		t.Fatal("current mod file should be deletable")
	}
	if fileops.CanDeleteCurrentModFile(ui, required.FullPath) {
		t.Fatal("required mod file should not be deletable")
	}
	if fileops.CanDeleteCurrentModFile(ui, sga.FullPath) {
		t.Fatal("SGA file should not be deletable")
	}
}

func folderSourceForTest() *modtree.FileSource {
	return &modtree.FileSource{Kind: modtree.KindThisMod}
}
