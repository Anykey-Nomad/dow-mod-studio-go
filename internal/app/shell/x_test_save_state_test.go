package shell_test

import (
	"dow-mod-studio/internal/app/shell"
	"testing"
)

func TestSaveEligibleOnlyForDirtyEditableTabs(t *testing.T) {
	ui := &shell.UiState{TabSel: 1, OpenTabs: []*shell.FileTab{{Path: `Data\script.lua`, Kind: "text"}}}
	if shell.SaveEligible(ui) {
		t.Fatal("clean text file must not enable Save")
	}
	shell.ActiveTab(ui).TextDirty = true
	if !shell.SaveEligible(ui) {
		t.Fatal("dirty text file must enable Save")
	}

	shell.ActiveTab(ui).Kind = "rgd"
	shell.ActiveTab(ui).TextDirty = false
	if shell.SaveEligible(ui) {
		t.Fatal("clean RGD file must not enable Save")
	}
	shell.ActiveTab(ui).RgdDirty = true
	if !shell.SaveEligible(ui) {
		t.Fatal("dirty RGD file must enable Save")
	}

	shell.ActiveTab(ui).Kind = "sgb"
	shell.ActiveTab(ui).RgdDirty = false
	if shell.SaveEligible(ui) {
		t.Fatal("clean SGB map must not enable Save")
	}
	shell.ActiveTab(ui).SgbDirty = true
	if !shell.SaveEligible(ui) {
		t.Fatal("dirty SGB map must enable Save")
	}

	shell.ActiveTab(ui).Kind = "fda"
	shell.ActiveTab(ui).SgbDirty = false
	if shell.SaveEligible(ui) {
		t.Fatal("FDA playback/export must not enable top Save")
	}
}
