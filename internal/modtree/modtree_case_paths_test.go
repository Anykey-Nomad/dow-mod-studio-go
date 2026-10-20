package modtree

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The game's .module files are written the way Windows reads them: paths with
// backslashes, and directory names whose case the filesystem folds. Neither
// survives a case-sensitive filesystem unchanged, and this install has both
// forms of the damage: UA_DE declares DataFolder.1 = Data while its files were
// written under a sibling lowercase data, and DoWDE ships its locale archive
// as %LOCALE%\DoWDELocale. On Windows each of those resolves to one folder or
// archive; here each one hides part of the mod.

const uaDEModulePath = "/mnt/windows/Program Files/Steam/steamapps/common/Dawn of War Definitive Edition/UA_DE.module"

// TestModulePathsResolveOnCaseSensitiveFS checks the two facts a reader of the
// tree depends on: the opened mod's own units are in the tree, and a required
// mod's backslash-pathed archive is among the sources.
func TestModulePathsResolveOnCaseSensitiveFS(t *testing.T) {
	if _, err := os.Stat(uaDEModulePath); err != nil {
		t.Skip("UA_DE is not installed here")
	}
	tree, err := BuildModTree(uaDEModulePath, "English")
	if err != nil {
		t.Fatalf("BuildModTree: %v", err)
	}

	// The mod's own unit file, which lives under the lowercase data folder.
	const own = `Data\attrib\ebps\races\necrons\structures\necron_amplification_generator.rgd`
	f := tree.FindFile(own)
	if f == nil {
		t.Fatalf("%s is not in the tree: the mod's own data folder was not registered", own)
	}
	if f.Source == nil || !strings.Contains(f.Source.DiskRoot, "UltimateApocalypse_THB_test") {
		got := "<nil>"
		if f.Source != nil {
			got = f.Source.DiskRoot
		}
		t.Errorf("%s resolved from %s, want the mod's own folder", own, got)
	}

	// DoWDE's locale archive is named with a backslash path in the .module.
	want := filepath.Join(filepath.Dir(uaDEModulePath), "DoWDE", "Locale", "English", "DoWDELocale.sga")
	if _, err := os.Stat(want); err != nil {
		t.Skipf("the locale archive is not where the test assumes it: %v", err)
	}
	found := false
	for _, s := range tree.Sources {
		// An archive source keeps its path in ArchivePath; DiskRoot is for
		// loose folders only.
		if s.IsSGA && strings.EqualFold(filepath.Clean(s.ArchivePath), filepath.Clean(want)) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("the locale archive %s was not registered as a source (backslash path in ArchiveFile.1)", want)
	}
}
