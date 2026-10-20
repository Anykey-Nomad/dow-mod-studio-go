package main

// The UA_DE module is the mod under test on this machine. This walks the whole
// unit pipeline end to end against it — locale table, screen name id scan,
// label, icon chain and model resolution — and reports what each step returns,
// so a break anywhere in the chain shows up as a log line rather than as a
// silently unlabelled row in the dialog.

import (
	"os"
	"strings"
	"testing"

	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/rgd"
)

const testUADEModule = "/mnt/windows/Program Files/Steam/steamapps/common/Dawn of War Definitive Edition/UA_DE.module"

// findUAUnit returns the unit with the given file name from any race.
func findUAUnit(t *testing.T, cat *unitCatalog, name string) unitEntry {
	t.Helper()
	for _, r := range cat.races {
		for _, u := range append(append([]unitEntry{}, r.troops...), r.structures...) {
			if u.name == name {
				return u
			}
		}
	}
	t.Fatalf("unit %q not in catalog (%d races)", name, len(cat.races))
	return unitEntry{}
}

func TestUADEDiagnoseUnitPipeline(t *testing.T) {
	if _, err := os.Stat(testUADEModule); err != nil {
		t.Skipf("UA_DE not installed: %v", err)
	}
	resetLocales(t)

	tree, err := modtree.BuildModTree(testUADEModule, "English")
	if err != nil {
		t.Fatalf("BuildModTree: %v", err)
	}
	defer tree.Close()
	// The same dictionary hookup loadModule does before anything parses a .rgd.
	rgd.AddDictRoot(tree.AppPath)
	t.Logf("sources=%d warnings=%d appPath=%q modFolder=%q", len(tree.Sources), len(tree.Warnings), tree.AppPath, tree.ModFolder)
	for _, w := range tree.Warnings {
		t.Logf("warning: %s", w)
	}

	// 1. The locale table, exactly as loadModule builds it.
	table := setLocale(tree.AppPath, testUADEModule, "English")
	t.Logf("locale table: files=%d lines=%d err=%v", table.Files, table.Lines, table.Err)
	for id, want := range map[string]string{
		"90100":  "", // engine screen name slot
		"705754": "", // Necron Lord on DXP3
	} {
		t.Logf("localisedString(%q) = %q (want non-empty %q)", id, localisedString(id), want)
	}

	// 2. The catalog and the background name scan.
	cat := buildUnitCatalog(tree)
	total := 0
	for _, r := range cat.races {
		total += r.unitCount()
	}
	t.Logf("catalog: %d races, %d units", len(cat.races), total)

	ui := newUI(unitTestTheme())
	ui.modTree = tree
	ui.modulePath = testUADEModule
	ui.gamePath = tree.AppPath
	ui.unitCatalog = cat
	ui.unitCatalogMod = tree.FileMapName
	ui.startUnitNameScan(tree, cat)
	waitForUnitNames(t, ui)
	t.Logf("scan: %d ids cached, done=%v", len(ui.unitNames), ui.unitNamesDone)

	// Coverage over the whole catalogue: how many units get a localised name,
	// a decoded icon and a model path, with a few misses of each kind logged
	// so a systemic gap cannot hide behind a good total.
	var noID, noName, noIcon, noModel int
	shownModel := 0
	for _, r := range cat.races {
		for _, u := range r.unitsOf() {
			if ui.unitNames[strings.ToLower(u.path)] == "" {
				noID++
			}
			if ui.unitLocalisedName(u) == "" {
				noName++
			}
			if ui.unitIcon(u) == nil {
				noIcon++
				if noIcon <= 5 {
					t.Logf("coverage: no icon for %s", u.path)
				}
			}
			d := loadUnitDetails(tree, u)
			src, ok := newUnitModelSource(tree, d)
			if !ok || unitModelPath(tree, src) == "" {
				noModel++
				if shownModel < 5 {
					shownModel++
					t.Logf("coverage: no model for %s (detailsErr=%q)", u.path, d.error)
				}
			}
		}
	}
	t.Logf("coverage over %d units: without id=%d, without localised name=%d, without icon=%d, without model=%d",
		total, noID, noName, noIcon, noModel)

	// 3. The three units the screenshots show, walked through every step.
	for _, name := range []string{"inquisition_damocles", "necron_basic_warrior", "necron_basic_warrior_advance_sp"} {
		u := findUAUnit(t, cat, name)
		id := ui.unitNames[strings.ToLower(u.path)]
		t.Logf("unit %s: id=%q localised=%q label=%q fileLabel=%q",
			u.name, id, ui.unitLocalisedName(u), ui.unitLabel(u), unitFileLabel(u))

		data, rerr := tree.ReadFile(u.path)
		if rerr != nil {
			t.Errorf("  ReadFile(%q): %v", u.path, rerr)
			continue
		}
		rf, perr := rgd.ParseRgd(data)
		if perr != nil {
			t.Errorf("  ParseRgd: %v", perr)
			continue
		}
		top := []string{}
		for _, e := range rf.Root.Children {
			top = append(top, e.Name)
		}
		t.Logf("  rgd top entries: %s", strings.Join(top, ", "))
		e := rgdFindPath(rf.Root, "ui_ext", "ui_info", "screen_name_id")
		t.Logf("  ui_ext=%v screen_name_id=%q animator=%q",
			rgdFindChild(rf.Root, "ui_ext") != nil,
			func() string {
				if e == nil {
					return "<missing>"
				}
				return e.DisplayValue()
			}(),
			unitAnimatorName(rf))

		// Icon chain: convention, by-name, then rgd icon_name.
		img := ui.unitIcon(u)
		if img != nil {
			t.Logf("  icon: ok bounds=%v", img.Bounds())
		} else {
			conv := `art\ui\ingame\` + unitTemplateIconBase(u.race) + `\` + u.name + "_icon.tga"
			t.Logf("  icon: MISSING; convention %q found=%v; byName %q found=%v; rgd icon_name=%q",
				conv, tree.FindFile(conv) != nil,
				u.name+"_icon.tga", tree.FindByName(u.name+"_icon.tga") != nil,
				rgdEntryStr(rgdFindPath(rf.Root, "ui_ext", "ui_info", "icon_name")))
		}

		// Model resolution as the viewport does it.
		d := loadUnitDetails(tree, u)
		if d.error != "" {
			t.Logf("  details: error %q", d.error)
			continue
		}
		src, ok := newUnitModelSource(tree, d)
		if !ok {
			t.Logf("  model: no source")
			continue
		}
		t.Logf("  model: animator=%q path=%q", src.animator, unitModelPath(tree, src))
	}
}
