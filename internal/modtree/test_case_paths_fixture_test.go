package modtree

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A .module is written the way Windows reads it: the folder it names and the
// Data it declares fold case, so one install works with any spelling. A
// case-sensitive filesystem takes the same names literally. A mod installed as
// casemodfolder while its .module says CaseModFolder — with its files under a
// lowercase data instead of the declared Data — contributed nothing to the
// tree, so the file tree and the unit list showed only the required mod. This
// is that whole case as a fixture, including where a save lands.
func TestModFolderAndDataFolderSpelledDifferentlyOnDisk(t *testing.T) {
	app := t.TempDir()
	const (
		moduleName = "CASEMOD"
		// What the .module declares, and what is on disk.
		declaredModFolder  = "CaseModFolder"
		diskModFolder      = "casemodfolder"
		declaredDataFolder = "Data"
		diskDataFolder     = "data"
	)
	module := "[global]\nModFolder=" + declaredModFolder + "\nDataFolder.1=" + declaredDataFolder + "\n"
	if err := os.WriteFile(filepath.Join(app, moduleName+".module"), []byte(module), 0o644); err != nil {
		t.Fatal(err)
	}
	unitDir := filepath.Join(app, diskModFolder, diskDataFolder, "attrib", "ebps", "races", "necrons", "troops")
	if err := os.MkdirAll(unitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	const unit = "case_custom_unit.rgd"
	if err := os.WriteFile(filepath.Join(unitDir, unit), []byte("fixture"), 0o644); err != nil {
		t.Fatal(err)
	}

	tree, err := BuildModTree(filepath.Join(app, moduleName+".module"), "English")
	if err != nil {
		t.Fatalf("BuildModTree: %v", err)
	}
	defer tree.Close()

	// The unit is in the tree, and it is the mod's own file rather than one
	// inherited from a required mod.
	want := `Data\attrib\ebps\races\necrons\troops\` + unit
	f := tree.FindFile(want)
	if f == nil {
		t.Fatalf("%s is not in the tree: the mod folder or its data folder was not registered", want)
	}
	if f.Source == nil || f.Source.Kind != KindThisMod {
		t.Fatalf("%s came from %+v, want the mod itself", want, f.Source)
	}
	dataDir := filepath.Join(app, diskModFolder, diskDataFolder)
	if got := filepath.Clean(f.Source.DiskRoot); !strings.EqualFold(got, filepath.Clean(dataDir)) {
		t.Errorf("%s resolved from %s, want the data folder that is on disk (%s)", want, got, dataDir)
	}
	// A save goes into the folder that is there, not into a second Data
	// created beside it from the spelling the .module uses.
	const saved = `Data\attrib\ebps\races\necrons\troops\case_saved_unit.rgd`
	disk, err := tree.WriteOverlayFile(saved, []byte("saved"))
	if err != nil {
		t.Fatalf("WriteOverlayFile: %v", err)
	}
	wantDisk := filepath.Join(dataDir, "attrib", "ebps", "races", "necrons", "troops", "case_saved_unit.rgd")
	if got := filepath.Clean(disk); got != filepath.Clean(wantDisk) {
		t.Errorf("save went to %s, want the data folder that is on disk (%s)", got, wantDisk)
	}
	if tree.FindFile(saved) == nil {
		t.Errorf("%s is not in the tree after saving it", saved)
	}
	// The mod folder still holds one data folder: two folders differing only
	// in case are one folder on Windows and two on a case-sensitive
	// filesystem, where the second would hold half the mod on its own.
	entries, err := os.ReadDir(filepath.Join(app, diskModFolder))
	if err != nil {
		t.Fatal(err)
	}
	folders := 0
	for _, e := range entries {
		if strings.EqualFold(e.Name(), diskDataFolder) {
			folders++
		}
	}
	if folders != 1 {
		t.Errorf("the mod folder holds %d data folders, want 1: %v", folders, entries)
	}
}
