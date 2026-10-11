package main

import "testing"

// The object interface is a singleton tab: asking for it twice brings the one
// tab forward instead of opening a second copy of the same browser.
func TestUnitTabIsSingleton(t *testing.T) {
	ui := &uiState{}
	if ui.unitTabIndex() != -1 {
		t.Fatal("a fresh ui must have no object tab")
	}
	ui.openUnitTab()
	if got := len(ui.openTabs); got != 1 {
		t.Fatalf("openTabs = %d, want 1", got)
	}
	if !ui.unitTabActive() {
		t.Error("the object tab should be active after opening it")
	}
	ui.openUnitTab()
	if got := len(ui.openTabs); got != 1 {
		t.Fatalf("opening twice produced %d tabs, want 1", got)
	}
}

// Selecting something else must make the object tab inactive, and coming back to
// it must make it active again — the tab bar is the only thing that decides.
func TestUnitTabTracksSelection(t *testing.T) {
	ui := &uiState{}
	ui.openUnitTab()
	ui.tabSel = 0
	if ui.unitTabActive() {
		t.Error("the object tab is still active with the Module tab selected")
	}
	ui.openUnitTab()
	if !ui.unitTabActive() {
		t.Error("reopening did not bring the object tab forward")
	}
}

// Closing the tab must leave the selection in a state the rest of the app can
// use, and reopening must land on the same unit rather than resetting it.
func TestUnitTabCloseKeepsSelection(t *testing.T) {
	ui := &uiState{modTree: testModTreeWithFiles(t, map[string]string{
		`Data\attrib\ebps\races\necrons\troops\necron_lord.rgd`:  "",
		`Data\attrib\ebps\races\necrons\troops\necron_lord2.rgd`: "",
	})}
	ui.openUnitTab()
	ui.unitSel = 1
	want := ui.currentUnit()
	if want == nil {
		t.Fatal("no unit selected to begin with")
	}
	ui.closeUnitTab()
	if ui.unitTabIndex() != -1 {
		t.Error("the object tab survived being closed")
	}
	if ui.unitTabActive() {
		t.Error("a closed object tab is still active")
	}
	// An open dropdown belongs to the tab that went away. Closing through the
	// tab bar's × calls closeTabAt directly, so that is the path exercised
	// here rather than closeUnitTab.
	ui.openUnitTab()
	ui.unitListOpen = true
	ui.closeTabAt(ui.unitTabIndex())
	if ui.unitListOpen || ui.unitRaceOpen {
		t.Error("a dropdown was left open after its tab closed")
	}
	ui.openUnitTab()
	if got := ui.currentUnit(); got == nil || got.name != want.name {
		t.Errorf("reopening landed on %v, want %v", got, want)
	}
}

// A file tab and the object tab coexist, and closing one leaves the other's
// selection alone.
func TestUnitTabCoexistsWithFileTabs(t *testing.T) {
	ui := &uiState{}
	ui.openTabs = append(ui.openTabs, &fileTab{kind: "rgd", path: `Data\attrib\x.rgd`})
	ui.ensureTabClicks()
	ui.openUnitTab()
	if got := len(ui.openTabs); got != 2 {
		t.Fatalf("openTabs = %d, want 2", got)
	}
	if ui.unitTabIndex() != 1 {
		t.Errorf("object tab index = %d, want 1", ui.unitTabIndex())
	}
	if ui.tabSel != 2 {
		t.Errorf("tabSel = %d, want the object tab selected", ui.tabSel)
	}
	// The file tab must still be the one before it.
	ui.tabSel = 1
	if got := ui.activeTab(); got == nil || got.kind != "rgd" {
		t.Errorf("activeTab = %+v, want the rgd tab", got)
	}
	if ui.unitTabActive() {
		t.Error("the object tab claims to be active while a file tab is selected")
	}
}

// The object tab is not backed by a file, so a mod reload must not close it and
// the session writer must not try to remember a path for it.
func TestUnitTabSurvivesModReloadAndIsNotPersisted(t *testing.T) {
	tab := &fileTab{kind: unitTabKind, path: unitTabPath}
	if !unitTabHasNoFile(tab) {
		t.Error("the object tab must count as having no file")
	}
	if !isUnitTab(tab) {
		t.Error("isUnitTab did not recognise the object tab")
	}
	if got := unitTabLabelFor(tab); got != unitTabLabel {
		t.Errorf("tab label = %q, want %q", got, unitTabLabel)
	}
	// A real file tab is not confused for the object tab.
	file := &fileTab{kind: "rgd", path: `Data\attrib\x.rgd`}
	if unitTabHasNoFile(file) {
		t.Error("a real file tab must not count as having no file")
	}
	if got := unitTabLabelFor(file); got != "" {
		t.Errorf("a file tab must not get the object label, got %q", got)
	}
}

// The session writer skips tabs with no file, so the object tab is not written
// out as a path that could not be reopened. The state file's location follows
// the config dir, which is redirected to a temp one so the real session file is
// not touched.
func TestObjectTabNotWrittenToSession(t *testing.T) {
	t.Setenv("AppData", t.TempDir())
	ui := newUI(unitTestTheme())
	ui.modTree = testModTreeWithFiles(t, map[string]string{
		`Data\attrib\ebps\races\necrons\troops\necron_lord.rgd`: "",
	})
	ui.openTabs = append(ui.openTabs, &fileTab{kind: "rgd", path: `Data\attrib\ebps\races\necrons\troops\necron_lord.rgd`})
	ui.openUnitTab()
	if err := savePersistedState(ui); err != nil {
		t.Fatalf("savePersistedState: %v", err)
	}
	saved, found, err := loadPersistedState()
	if err != nil {
		t.Fatalf("loadPersistedState: %v", err)
	}
	if !found {
		t.Fatal("the state file was not written")
	}
	if len(saved.Tabs) != 1 {
		t.Fatalf("persisted %d tabs, want only the file tab: %+v", len(saved.Tabs), saved.Tabs)
	}
	if saved.Tabs[0].Path == "" {
		t.Error("the file tab lost its path")
	}
}
