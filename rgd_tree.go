package main

import (
	"dow-mod-studio/internal/rgd"
	"fmt"
	"log"
	"path/filepath"
	"strings"

	"gioui.org/widget"
)

func (ui *uiState) openRgd(virtualPath string) {
	if ui.modTree == nil {
		return
	}
	if idx := ui.findTabIndex(virtualPath); idx >= 0 {
		ui.tabSel = idx + 1
		ui.ensureTabClicks()
		ui.statusText = fmt.Sprintf("Opened %s", filepath.Base(virtualPath))
		return
	}
	data, err := ui.modTree.ReadFile(virtualPath)
	if err != nil {
		t := &fileTab{path: virtualPath, kind: "rgd", rgdError: err.Error(), rgdExpanded: map[string]bool{"GameData": true}, rgdSelected: "GameData"}
		ui.openTabs = append(ui.openTabs, t)
		ui.tabSel = len(ui.openTabs)
		ui.ensureTabClicks()
		ui.statusText = fmt.Sprintf("Failed to read %s: %v", filepath.Base(virtualPath), err)
		return
	}
	rf, err := rgd.ParseRgd(data)
	if err != nil {
		t := &fileTab{path: virtualPath, kind: "rgd", rgdError: err.Error(), rgdExpanded: map[string]bool{"GameData": true}, rgdSelected: "GameData"}
		ui.openTabs = append(ui.openTabs, t)
		ui.tabSel = len(ui.openTabs)
		ui.ensureTabClicks()
		ui.statusText = fmt.Sprintf("Failed to parse %s: %v", filepath.Base(virtualPath), err)
		return
	}
	t := &fileTab{
		path: virtualPath, kind: "rgd",
		rgdFile: rf, rgdError: "",
		rgdExpanded:         map[string]bool{"GameData": true},
		rgdSelected:         "GameData",
		rgdValEditors:       make(map[string]*widget.Editor),
		rgdValErrors:        make(map[string]string),
		rgdValClicks:        make(map[string]*widget.Clickable),
		rgdSuggestionClicks: make(map[string][]widget.Clickable),
	}
	t.rgdRefEditor.SingleLine = true
	t.rgdRefEditor.Submit = true
	ui.openTabs = append(ui.openTabs, t)
	ui.tabSel = len(ui.openTabs)
	ui.ensureTabClicks()
	ui.rebuildRgdMidNodesFor(t)
	ui.ensureRgdMidClicks(t, len(t.rgdMidNodes))
	ui.statusText = fmt.Sprintf("Opened %s — %d top-level entries", filepath.Base(virtualPath), len(rf.Root.Children))
	log.Printf("openRgd %q: %d top entries", virtualPath, len(rf.Root.Children))
}

func (ui *uiState) ensureRgdMidClicks(t *fileTab, n int) {
	if len(t.rgdMidClicks) < n {
		old := t.rgdMidClicks
		t.rgdMidClicks = make([]widget.Clickable, n)
		copy(t.rgdMidClicks, old)
	} else if len(t.rgdMidClicks) > n {
		t.rgdMidClicks = t.rgdMidClicks[:n]
	}
}

func (ui *uiState) rebuildRgdMidNodes() {
	t := ui.activeTab()
	if t == nil || t.kind != "rgd" {
		return
	}
	ui.rebuildRgdMidNodesFor(t)
}

func (ui *uiState) rebuildRgdMidNodesFor(t *fileTab) {
	if t.rgdFile == nil || t.rgdFile.Root == nil {
		t.rgdMidNodes = nil
		return
	}
	if t.rgdExpanded == nil {
		t.rgdExpanded = map[string]bool{"GameData": true}
	}
	if t.rgdSelected == "" {
		t.rgdSelected = "GameData"
	}
	var out []rgdVisible
	root := t.rgdFile.Root
	out = append(out, rgdVisible{
		Name:     root.Name,
		Path:     "GameData",
		Depth:    0,
		IsTable:  root.IsTable(),
		HasKids:  len(root.Children) > 0,
		Expanded: t.rgdExpanded["GameData"],
		Selected: t.rgdSelected == "GameData",
		Type:     root.Type,
		Ref:      root.RefString(),
		Entry:    root,
	})
	if t.rgdExpanded["GameData"] {
		ui.rgdFlattenChildrenFor(t, root.Children, "GameData", 1, &out)
	}
	t.rgdMidNodes = out
}

func (ui *uiState) rgdFlattenChildren(children []*rgd.RgdEntry, basePath string, depth int, out *[]rgdVisible) {
	t := ui.activeTab()
	if t == nil {
		return
	}
	ui.rgdFlattenChildrenFor(t, children, basePath, depth, out)
}

func (ui *uiState) rgdFlattenChildrenFor(t *fileTab, children []*rgd.RgdEntry, basePath string, depth int, out *[]rgdVisible) {
	for _, ch := range children {
		path := basePath + "/" + ch.Name
		expanded := t.rgdExpanded[path]
		*out = append(*out, rgdVisible{
			Name:     ch.Name,
			Path:     path,
			Depth:    depth,
			IsTable:  ch.IsTable(),
			HasKids:  len(ch.Children) > 0,
			Expanded: expanded,
			Selected: t.rgdSelected == path,
			Type:     ch.Type,
			Ref:      ch.RefString(),
			Entry:    ch,
		})
		if ch.IsTable() && len(ch.Children) > 0 && expanded {
			ui.rgdFlattenChildrenFor(t, ch.Children, path, depth+1, out)
		}
	}
}

func (ui *uiState) rgdFindEntry(path string) *rgd.RgdEntry {
	t := ui.activeTab()
	if t == nil || t.kind != "rgd" {
		return nil
	}
	return ui.rgdFindEntryFor(t, path)
}

func (ui *uiState) rgdFindEntryFor(t *fileTab, path string) *rgd.RgdEntry {
	if t.rgdFile == nil || t.rgdFile.Root == nil {
		return nil
	}
	if path == "GameData" || path == "" {
		return t.rgdFile.Root
	}
	parts := strings.Split(path, "/")
	if len(parts) == 0 || parts[0] != "GameData" {
		return nil
	}
	cur := t.rgdFile.Root
	for _, seg := range parts[1:] {
		found := false
		for _, ch := range cur.Children {
			if ch.Name == seg || strings.EqualFold(ch.Name, seg) {
				cur = ch
				found = true
				break
			}
		}
		if !found {
			return nil
		}
	}
	return cur
}

func (ui *uiState) rgdSelectedEntry() *rgd.RgdEntry {
	t := ui.activeTab()
	if t == nil || t.kind != "rgd" {
		return nil
	}
	return ui.rgdSelectedEntryFor(t)
}

func (ui *uiState) rgdSelectedEntryFor(t *fileTab) *rgd.RgdEntry {
	if t.rgdFile == nil {
		return nil
	}
	if e := ui.rgdFindEntryFor(t, t.rgdSelected); e != nil {
		return e
	}
	return t.rgdFile.Root
}
