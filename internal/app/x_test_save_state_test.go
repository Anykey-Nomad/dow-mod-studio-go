package app

import "testing"

func TestSaveEligibleOnlyForDirtyEditableTabs(t *testing.T) {
	ui := &uiState{tabSel: 1, openTabs: []*fileTab{{path: `Data\script.lua`, kind: "text"}}}
	if ui.saveEligible() {
		t.Fatal("clean text file must not enable Save")
	}
	ui.activeTab().textDirty = true
	if !ui.saveEligible() {
		t.Fatal("dirty text file must enable Save")
	}

	ui.activeTab().kind = "rgd"
	ui.activeTab().textDirty = false
	if ui.saveEligible() {
		t.Fatal("clean RGD file must not enable Save")
	}
	ui.activeTab().rgdDirty = true
	if !ui.saveEligible() {
		t.Fatal("dirty RGD file must enable Save")
	}

	ui.activeTab().kind = "sgb"
	ui.activeTab().rgdDirty = false
	if ui.saveEligible() {
		t.Fatal("clean SGB map must not enable Save")
	}
	ui.activeTab().sgbDirty = true
	if !ui.saveEligible() {
		t.Fatal("dirty SGB map must enable Save")
	}

	ui.activeTab().kind = "fda"
	ui.activeTab().sgbDirty = false
	if ui.saveEligible() {
		t.Fatal("FDA playback/export must not enable top Save")
	}
}
