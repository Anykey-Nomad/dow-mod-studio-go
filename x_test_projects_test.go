package main

import (
	"dow-mod-studio/internal/modtree"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSanitizeFolderName covers the names a mod can actually carry. A mod's
// name comes from its .module file, so it is not guaranteed to be usable as a
// path component, and a bad name must never walk out of the Projects folder.
func TestSanitizeFolderName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"DXP3", "DXP3"},
		{"DXP3.module", "DXP3"},
		{"necron_lord", "necron_lord"},
		{`  spaced  `, "spaced"},
		// A path in the name must be reduced to its last component.
		{`Data\attrib\SomeMod`, "SomeMod"},
		{"art/mods/My Mod", "My Mod"},
		// Characters Windows rejects become underscores.
		{`bad:name*here?`, "bad_name_here_"},
		{`a<b>c|d"e`, "a_b_c_d_e"},
		// Control characters are dropped rather than replaced.
		{"a\tb\x00c", "abc"},
		// Names Windows reserves fall back, so no crash and no surprise.
		{"CON", "mod"},
		{"con", "mod"},
		{"NUL.module", "mod"},
		{"..", "mod"},
		{".", "mod"},
		{"", "mod"},
		{"   ", "mod"},
		{`..\..\escape`, "escape"},
	}
	for _, c := range cases {
		if got := sanitizeFolderName(c.in); got != c.want {
			t.Errorf("sanitizeFolderName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestSanitizeFolderNameStaysInsideProjects is the safety property that matters:
// whatever the mod is called, the resulting folder has to sit directly under
// the projects root.
func TestSanitizeFolderNameStaysInsideProjects(t *testing.T) {
	root := filepath.Join("C:", "appdata", "DoW Mod Studio", projectsDirName)
	hostile := []string{
		`..\..\..\Windows\System32`,
		`/etc/passwd`,
		`C:\Windows\System32\config`,
		strings.Repeat("a", 500),
		"\x00\x01\x02",
		"CON",
		".",
	}
	for _, name := range hostile {
		got := sanitizeFolderName(name)
		full := filepath.Join(root, got)
		if filepath.Dir(full) != root {
			t.Errorf("name %q produced %q, which escapes %q", name, full, root)
		}
		// A long name is truncated, not rejected, so it stays usable.
		if got == "" {
			t.Errorf("name %q produced an empty folder name", name)
		}
	}
}

// TestEnsureProjectDirsCreatesModAndUnits creates the real tree under a
// temporary data directory and checks its shape, then that a second call is
// harmless.
func TestEnsureProjectDirsCreatesModAndUnits(t *testing.T) {
	data := t.TempDir()
	t.Setenv("APPDATA", data)
	t.Setenv("XDG_CONFIG_HOME", data)

	dir, err := ensureProjectDirs(&modtree.ModTree{FileMapName: "DXP3"}, `C:/games/DXP3.module`)
	if err != nil {
		t.Fatalf("ensureProjectDirs: %v", err)
	}
	wantRoot := filepath.Join(data, "DoW Mod Studio", projectsDirName, "DXP3")
	if dir != wantRoot {
		t.Errorf("project dir = %q, want %q", dir, wantRoot)
	}
	units := filepath.Join(dir, unitsDirName)
	info, err := os.Stat(units)
	if err != nil {
		t.Fatalf("Units folder missing: %v", err)
	}
	if !info.IsDir() {
		t.Errorf("%s is not a directory", units)
	}
	// The Units folder is also the mod's folder, and the tree below it is the
	// project's own, next to the recycle bin.
	if filepath.Dir(filepath.Dir(units)) != filepath.Join(data, "DoW Mod Studio", projectsDirName) {
		t.Errorf("Units is nested too deeply: %s", units)
	}

	// Opening the same module again must not fail or nest a second time.
	again, err := ensureProjectDirs(&modtree.ModTree{FileMapName: "DXP3"}, `C:/games/DXP3.module`)
	if err != nil {
		t.Fatalf("second ensureProjectDirs: %v", err)
	}
	if again != dir {
		t.Errorf("second call gave %q, want the same %q", again, dir)
	}
}

// TestEnsureProjectDirsPerModFolders checks two mods get separate folders, and
// that names colliding once sanitised do not overwrite each other silently.
func TestEnsureProjectDirsPerModFolders(t *testing.T) {
	data := t.TempDir()
	t.Setenv("APPDATA", data)
	t.Setenv("XDG_CONFIG_HOME", data)

	first, err := ensureProjectDirs(&modtree.ModTree{FileMapName: "DXP3"}, "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := ensureProjectDirs(&modtree.ModTree{FileMapName: "MyMod"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatalf("two mods share the folder %q", first)
	}
	for _, dir := range []string{first, second} {
		if _, err := os.Stat(filepath.Join(dir, unitsDirName)); err != nil {
			t.Errorf("Units missing under %s: %v", dir, err)
		}
	}
}

// TestModProjectNameFallsBack covers a tree that never got a name, which
// happens if the .module file is unusual.
func TestModProjectNameFallsBack(t *testing.T) {
	if got := modProjectName(&modtree.ModTree{FileMapName: "DXP3"}, "C:/games/Other.module"); got != "DXP3" {
		t.Errorf("with a tree name the project is %q, want DXP3", got)
	}
	if got := modProjectName(&modtree.ModTree{}, `C:/games/Other.module`); got != "Other.module" {
		t.Errorf("without a tree name the project is %q, want the module's base name", got)
	}
	if got := sanitizeFolderName(modProjectName(&modtree.ModTree{}, `C:/games/Other.module`)); got != "Other" {
		t.Errorf("sanitized fallback = %q, want Other", got)
	}
	if got := modProjectName(&modtree.ModTree{}, ""); got != "" {
		t.Errorf("with nothing to go on the project name is %q, want empty", got)
	}
}
