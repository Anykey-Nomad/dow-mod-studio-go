//go:build windows

package main

import (
	"fmt"
	"strings"
	"testing"
)

// TestUnitNamesOnTheRealModule runs the whole thing against the installed
// Dawn of War module: the scan reads every unit's screen_name_id, the label
// resolves it through the English string table, and switching the language
// relabels from the same cache without reading a single .rgd again.
func TestUnitNamesOnTheRealModule(t *testing.T) {
	resetLocales(t)
	gamePath := testGameFolder(t)
	setLocale(gamePath, "", "English")

	tree := openTestDXP3(t)
	cat := buildUnitCatalog(tree)
	if cat == nil || len(cat.races) == 0 {
		t.Skip("no races in the module")
	}
	total := 0
	for _, r := range cat.races {
		total += len(r.troops) + len(r.structures)
	}

	ui := newUI(unitTestTheme())
	// setLocaleLanguage finds the .ucs files through the game folder, so the
	// state needs one, not just the table the test installed by hand.
	ui.gamePath = gamePath
	ui.modulePath = tree.AppPath
	ui.modTree = tree
	ui.unitCatalog = cat
	ui.unitCatalogMod = tree.FileMapName
	ui.startUnitNameScan(tree, cat)
	waitForUnitNames(t, ui)

	// Every unit that carries an id should have it cached. Measured on DXP3:
	// 582 units, all but a handful carrying one, so a scan that resolved far
	// fewer means the path into ui_info is wrong, not that the mod is sparse.
	if len(ui.unitNames) < total-8 {
		t.Errorf("the scan resolved %d ids for %d units; the walk into ui_ext\\ui_info is probably wrong",
			len(ui.unitNames), total)
	}

	// Every race's units should mostly resolve to real text in English.
	for _, r := range cat.races {
		units := r.unitsOf()
		resolved := 0
		for _, u := range units {
			if ui.unitLocalisedName(u) != "" {
				resolved++
			}
		}
		if resolved == 0 {
			t.Errorf("race %q resolved no names at all", r.name)
		}
		if resolved < len(units)-4 {
			t.Errorf("race %q resolved %d of %d names", r.name, resolved, len(units))
		}
	}

	// The label is the text, never the id, and the fallback is the file name.
	lord := findRealUnit(t, cat, "necron_lord")
	got := ui.unitLabel(lord)
	if got == "necron_lord" {
		t.Errorf("necron_lord still reads as its file name; the English table has %q",
			localisedString("705754"))
	}
	if strings.Contains(got, "$") {
		t.Errorf("the label %q shows a localisation id", got)
	}
	if unitFileLabel(lord) != "necron_lord" {
		t.Errorf("the dim file name reads %q", unitFileLabel(lord))
	}
	t.Logf("necron_lord reads %q (file %q)", got, unitFileLabel(lord))

	// Switching the language must relabel from the cache. The index is keyed by
	// id and holds no text, so this is the whole point of caching the id.
	english := ui.unitLabel(lord)
	ui.setLocaleLanguage("Russian")
	russian := ui.unitLabel(lord)
	if russian == english {
		t.Errorf("switching to Russian left the label as %q", russian)
	}
	if ui.unitNamesRunning {
		t.Error("switching the language restarted the scan; the cached ids should have been enough")
	}
	if russian == "necron_lord" {
		t.Error("the Russian label fell back to the file name")
	}
	t.Logf("Russian: %q", russian)
	ui.setLocaleLanguage("English")

	// And back again, to catch a table swap that did not restore.
	if again := ui.unitLabel(lord); again != english {
		t.Errorf("switching back gave %q, want %q", again, english)
	}
}

// TestRealUnitsShareNamesWithinARace is why the file name stays on the row.
// Measured on DXP3: several units of one race resolve to the same text, so a
// list showing only the display name cannot tell them apart.
func TestRealUnitsShareNamesWithinARace(t *testing.T) {
	resetLocales(t)
	setLocale(testGameFolder(t), "", "English")
	tree := openTestDXP3(t)
	cat := buildUnitCatalog(tree)
	ui := newUI(unitTestTheme())
	ui.modTree = tree
	ui.unitCatalog = cat
	ui.unitCatalogMod = tree.FileMapName
	ui.startUnitNameScan(tree, cat)
	waitForUnitNames(t, ui)

	shared := 0
	for _, r := range cat.races {
		seen := map[string][]string{}
		for _, u := range r.unitsOf() {
			seen[ui.unitLabel(u)] = append(seen[ui.unitLabel(u)], u.name)
		}
		for label, files := range seen {
			if len(files) > 1 {
				shared++
				if shared <= 3 {
					t.Logf("race %q: %d units all read %q (%s)", r.name, len(files), label, strings.Join(files, ", "))
				}
			}
		}
	}
	if shared == 0 {
		t.Skip("no shared display names in this module, so the file name is not carrying anything here")
	}
	fmt.Printf("%d groups of units share a display name, which is why the file name is on the row\n", shared)
}

func findRealUnit(t *testing.T, cat *unitCatalog, name string) unitEntry {
	t.Helper()
	for _, r := range cat.races {
		for _, u := range append(append([]unitEntry{}, r.troops...), r.structures...) {
			if u.name == name {
				return u
			}
		}
	}
	t.Fatalf("no unit named %q in the module", name)
	return unitEntry{}
}
