package modtree

import (
	"fmt"
	"os"
	"testing"
)

// The path the game installs to on Windows; another drive or install shows
// up here as a skip rather than a failure.
const dxp3ModulePath = `C:/Program Files/Steam/steamapps/common/Dawn of War Definitive Edition/DXP3.module`

func TestBuildModTree_DXP3(t *testing.T) {
	if _, err := os.Stat(dxp3ModulePath); err != nil {
		t.Skip("DXP3 is not installed here")
	}
	path := dxp3ModulePath
	tree, err := BuildModTree(path, "English")
	if err != nil {
		t.Fatalf("BuildModTree: %v", err)
	}
	defer tree.Close()
	f, fi := tree.Count()
	t.Logf("loaded %s: %d folders, %d files, %d sources", path, f, fi, len(tree.Sources))
	for _, s := range tree.Sources {
		t.Logf("  source priority=%d kind=%d mod=%q src=%q sga=%v", s.Priority, s.Kind, s.ModName, s.SourceName, s.IsSGA)
	}
	if len(tree.Warnings) > 0 {
		for _, w := range tree.Warnings {
			t.Logf("warn: %s", w)
		}
	}
	if f == 0 || fi == 0 {
		t.Fatalf("expected non-empty tree")
	}
	// flatten check
	nodes := tree.Flatten(map[string]bool{"Data": true}, "")
	t.Logf("visible nodes (Data expanded only): %d", len(nodes))
	for i, n := range nodes {
		if i < 20 {
			kind := "dir"
			if n.IsFile {
				kind = fmt.Sprintf("file kind=%d mod=%s", n.Kind, n.ModName)
			}
			t.Logf("  [%d] depth=%d %s %q (%s)", i, n.Depth, kind, n.Name, n.FullPath)
		}
	}
	// check that attrib exists somewhere
	foundAttrib := false
	for _, c := range tree.Root.Children {
		if c.Name == "attrib" {
			foundAttrib = true
		}
	}
	if !foundAttrib {
		// maybe under Data children via case sensitivity?
		if len(tree.Root.Children) > 0 {
			for _, c := range tree.Root.Children {
				t.Logf("top-level child: %q has %d children %d files", c.Name, len(c.Children), len(c.Files))
			}
		}
		t.Logf("WARNING: attrib not found at top level; dumping root children done")
	}
}

func TestParseModule_DXP3(t *testing.T) {
	if _, err := os.Stat(dxp3ModulePath); err != nil {
		t.Skip("DXP3 is not installed here")
	}
	m, err := ParseModule(dxp3ModulePath)
	if err != nil {
		t.Fatalf("ParseModule: %v", err)
	}
	t.Logf("UIName=%q ModFolder=%q", m.UIName, m.ModFolder)
	t.Logf("DataFolders: %v", m.DataFolders)
	t.Logf("Archives: %v", m.Archives)
	t.Logf("Required: %v", m.RequiredMods)
	if len(m.DataFolders) == 0 {
		t.Fatalf("expected DataFolders")
	}
}

func TestResolveDynamics(t *testing.T) {
	got := ResolveDynamics("%LOCALE%\\Data", "English", "")
	if got != "Locale\\English\\Data" {
		t.Fatalf("got %q", got)
	}
	got2 := ResolveDynamics("W40kData", "English", ".sga")
	if got2 != "W40kData.sga" {
		t.Fatalf("got %q", got2)
	}
}
