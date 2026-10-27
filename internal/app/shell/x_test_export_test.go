package shell_test

import (
	"dow-mod-studio/internal/app/editors"
	"dow-mod-studio/internal/app/shell"
	"dow-mod-studio/internal/modtree"
	"os"
	"path/filepath"
	"testing"
)

func testExportTree(t *testing.T) (*shell.UiState, *modtree.ModTree, string) {
	t.Helper()
	appPath := t.TempDir()
	tree := &modtree.ModTree{
		Root:        modtree.NewFolder("Data", "Data", nil),
		FileMapName: "TestMod",
		AppPath:     appPath,
	}
	folder := tree.GetOrCreateFolder(tree.Root, "attrib", nil)
	source := &modtree.FileSource{ModName: "ParentMod", Kind: modtree.KindReqMod, IsSGA: true}
	file := &modtree.TreeFile{
		Name:      "example.rgd",
		FullPath:  "Data\\attrib\\example.rgd",
		Source:    source,
		SGAFileID: 3,
	}
	folder.Files = append(folder.Files, file)
	folder.FileByLower[file.Name] = file
	ui := &shell.UiState{ModTree: tree, TabSel: 1, OpenTabs: []*shell.FileTab{{Path: file.FullPath, Kind: "rgd"}}}
	return ui, tree, file.FullPath
}

func TestWavToFDAUsesCurrentModFolder(t *testing.T) {
	_, tree, _ := testExportTree(t)
	wavPath := `Data\sound\music\track.wav`
	fdaPath := editors.WavTargetFDA(wavPath)
	disk, err := editors.WriteFDAOverlay(tree, fdaPath, []byte("fake FDA"))
	if err != nil {
		t.Fatal(err)
	}
	overlay, err := tree.OverlayPath(fdaPath)
	if err != nil {
		t.Fatal(err)
	}
	if disk != overlay {
		t.Fatalf("FDA written to %q, expected overlay %q", disk, overlay)
	}
	if filepath.Dir(disk) != filepath.Join(tree.AppPath, tree.FileMapName, "Data", "sound", "music") {
		t.Fatalf("FDA was written outside current mod folder: %s", disk)
	}
	if _, err := os.Stat(disk); err != nil {
		t.Fatalf("converted FDA does not exist: %v", err)
	}
}

func TestExportEligible_SGAWithoutCurrentOverlay(t *testing.T) {
	ui, _, path := testExportTree(t)
	eligible, reason := shell.ExportEligible(ui, shell.ActiveTab(ui))
	if !eligible {
		t.Fatalf("expected SGA file to be exportable: %s", reason)
	}
	overlay, err := ui.ModTree.OverlayPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(overlay); !os.IsNotExist(err) {
		t.Fatalf("expected overlay file to be absent, stat error=%v", err)
	}
}

func TestExportEligible_BlockedByCurrentOverlay(t *testing.T) {
	ui, tree, path := testExportTree(t)
	if _, err := tree.WriteOverlayFile(path, []byte("overlay")); err != nil {
		t.Fatal(err)
	}
	eligible, reason := shell.ExportEligible(ui, shell.ActiveTab(ui))
	if eligible {
		t.Fatal("expected file already present in current mod to be blocked")
	}
	if reason == "" {
		t.Fatal("expected blocked reason")
	}
}

func TestExportEligible_FSSourceNotEligible(t *testing.T) {
	ui, tree, path := testExportTree(t)
	file := tree.FindFile(path)
	file.Source.IsSGA = false
	file.Source.Kind = modtree.KindThisMod
	eligible, reason := shell.ExportEligible(ui, shell.ActiveTab(ui))
	if eligible {
		t.Fatal("expected filesystem source to be blocked")
	}
	if reason == "" {
		t.Fatal("expected blocked reason")
	}
}
