package main

import (
	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/rgd"
	"path/filepath"
	"testing"
)

func TestRgdParseMonolith(t *testing.T) {
	tree, err := modtree.BuildModTree(`C:/Program Files/Steam/steamapps/common/Dawn of War Definitive Edition/DXP3.module`, "English")
	if err != nil {
		t.Fatalf("modtree.BuildModTree: %v", err)
	}
	defer tree.Close()
	// find any .rgd
	var rgdPath string
	var search func(folder *modtree.TreeFolder)
	search = func(folder *modtree.TreeFolder) {
		if rgdPath != "" {
			return
		}
		for _, f := range folder.Files {
			if filepath.Ext(f.Name) == ".rgd" && f.Name == "monolith.rgd" {
				rgdPath = f.FullPath
				return
			}
		}
		for _, c := range folder.Children {
			search(c)
			if rgdPath != "" {
				return
			}
		}
	}
	search(tree.Root)
	if rgdPath == "" {
		// try any rgd
		search = func(folder *modtree.TreeFolder) {
			if rgdPath != "" {
				return
			}
			for _, f := range folder.Files {
				if filepath.Ext(f.Name) == ".rgd" {
					rgdPath = f.FullPath
					return
				}
			}
			for _, c := range folder.Children {
				search(c)
			}
		}
		search(tree.Root)
	}
	if rgdPath == "" {
		t.Fatalf("no rgd found")
	}
	t.Logf("found rgd at %q", rgdPath)
	// read bytes
	data, err := tree.ReadFile(rgdPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	t.Logf("size %d", len(data))
	// hex dump first 64
	for i := 0; i < 64 && i < len(data); i++ {
		if i%16 == 0 {
			t.Logf("\n%04x:", i)
		}
		t.Logf(" %02x", data[i])
	}
	t.Logf(" header %q", string(data[0:16]))
	rf, err := rgd.ParseRgd(data)
	if err != nil {
		t.Fatalf("rgd.ParseRgd: %v", err)
	}
	t.Logf("Parsed %q descriptor %q chunkVer %d root children %d", rf.Root.Name, rf.Descriptor, rf.ChunkVersion, len(rf.Root.Children))
	for i, ch := range rf.Root.Children {
		if i < 30 {
			ref := ""
			if ch.Ref != nil {
				ref = ch.Ref.DisplayValue()
			}
			t.Logf(" child[%d] %q type=%s hash=%08x ref=%q children=%d val=%q", i, ch.Name, ch.TypeLabel(), ch.Hash, ref, len(ch.Children), ch.DisplayValue())
		}
	}
	// check expected children as in screenshot
	wantNames := []string{"ability_ext", "health_ext", "ui_ext"}
	found := map[string]bool{}
	for _, ch := range rf.Root.Children {
		found[ch.Name] = true
	}
	for _, n := range wantNames {
		if !found[n] {
			t.Logf("WARNING expected child %q missing", n)
		}
	}
	// also test nested: health_ext children?
	for _, ch := range rf.Root.Children {
		if ch.Name == "health_ext" {
			t.Logf(" health_ext children %d", len(ch.Children))
			for _, sub := range ch.Children {
				t.Logf("  health sub %q type %s val %q children %d", sub.Name, sub.TypeLabel(), sub.DisplayValue(), len(sub.Children))
			}
			break
		}
	}
	// try to verify Reference parsing: health_ext ref?
	for _, ch := range rf.Root.Children {
		if ch.Name == "health_ext" {
			if ch.Ref != nil {
				t.Logf(" health_ext ref type %s val %q", ch.Ref.TypeLabel(), ch.Ref.DisplayValue())
			} else {
				t.Logf(" health_ext no ref")
			}
		}
	}
}
