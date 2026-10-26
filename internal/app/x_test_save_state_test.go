package app

import "testing"

func TestSaveEligibleOnlyForDirtyEditableTabs(t *testing.T) {
	ui := &uiState{TabSel: 1, OpenTabs: []*fileTab{{Path: `Data\script.lua`, Kind: "text"}}}
	if SaveEligible(ui) {
		t.Fatal("clean text file must not enable Save")
	}
	ActiveTab(ui).TextDirty = true
	if !SaveEligible(ui) {
		t.Fatal("dirty text file must enable Save")
	}

	ActiveTab(ui).Kind = "rgd"
	ActiveTab(ui).TextDirty = false
	if SaveEligible(ui) {
		t.Fatal("clean RGD file must not enable Save")
	}
	ActiveTab(ui).RgdDirty = true
	if !SaveEligible(ui) {
		t.Fatal("dirty RGD file must enable Save")
	}

	ActiveTab(ui).Kind = "sgb"
	ActiveTab(ui).RgdDirty = false
	if SaveEligible(ui) {
		t.Fatal("clean SGB map must not enable Save")
	}
	ActiveTab(ui).SgbDirty = true
	if !SaveEligible(ui) {
		t.Fatal("dirty SGB map must enable Save")
	}

	ActiveTab(ui).Kind = "fda"
	ActiveTab(ui).SgbDirty = false
	if SaveEligible(ui) {
		t.Fatal("FDA playback/export must not enable top Save")
	}
}
