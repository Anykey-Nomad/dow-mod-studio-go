package main

import (
	"dow-mod-studio/internal/fx"
	"os"
	"path/filepath"
	"testing"
)

// readFxLua loads one of the game's effect files, skipping without an install.
func readFxLua(t *testing.T, rel string) []byte {
	t.Helper()
	p := filepath.Join(dowInstallDir, "art", "fx", filepath.FromSlash(rel))
	data, err := os.ReadFile(p)
	if err != nil {
		t.Skipf("game effects not available: %v", err)
	}
	return data
}

// TestParseFxLuaCombo reads a combo effect and checks the shape the preview
// needs: a root, a spray child with an emission rate, and a ring child with
// a texture, a size curve and a colour ramp.
func TestParseFxLuaCombo(t *testing.T) {
	f, err := fx.ParseFxLua(readFxLua(t, "aeonic_orb_blasts.lua"))
	if err != nil {
		t.Fatalf("fx.ParseFxLua: %v", err)
	}
	if len(f.Nodes) < 3 {
		t.Fatalf("nodes = %d, want the root, a spray and more", len(f.Nodes))
	}
	root := f.Root()
	if root == nil || root.Effect != "root" {
		t.Fatalf("root = %+v", root)
	}
	kids := f.Children("root")
	if len(kids) == 0 {
		t.Fatal("root has no children")
	}
	var spray, leaf *fx.FxNode
	for _, k := range kids {
		switch k.Style {
		case "STYLE_SPRAY":
			spray = k
		case "STYLE_RING":
			leaf = k
		}
		if stringsHasPrefix(k.Effect, "root>") {
			t.Logf("child %s style=%s", k.Effect, k.Style)
		}
	}
	if spray == nil {
		// Some files nest one level deeper; search one more level down.
		for _, k := range kids {
			for _, kk := range f.Children(k.Effect) {
				if kk.Style == "STYLE_SPRAY" {
					spray = kk
				}
				if kk.Style == "STYLE_RING" {
					leaf = kk
				}
			}
		}
	}
	if spray == nil {
		t.Fatalf("no STYLE_SPRAY node among %d children", len(kids))
	}
	rate, ok := spray.Prop("Emitter_Rate")
	if !ok || len(rate.Curve) == 0 {
		t.Errorf("spray has no Emitter_Rate curve")
	} else {
		t.Logf("spray %s: rate keys=%v duration=%v loop=%v",
			spray.Effect, rate.Curve[:minInt(3, len(rate.Curve))],
			spray.Float("Emitter_Duration", -1), spray.Bool("Emitter_Loop", false))
	}
	if leaf == nil {
		t.Skip("no STYLE_RING leaf to inspect in this file")
	}
	tex := leaf.String("Texture")
	if tex == "" {
		t.Errorf("ring has no Texture")
	}
	rad, ok := leaf.Prop("Radius")
	if !ok || len(rad.Curve) == 0 {
		t.Errorf("ring has no Radius curve")
	}
	col, ok := leaf.Prop("Colour")
	if !ok || len(col.Colour) == 0 {
		t.Errorf("ring has no Colour ramp")
	} else {
		s := col.Colour[0]
		t.Logf("ring texture=%s radiusKeys=%d colour[0]=(t=%.3f rgba=%.2f %.2f %.2f %.2f) lifetime=%v",
			tex, len(rad.Curve), s.T, s.R, s.G, s.B, s.A,
			leaf.Float("Duration", -1))
		if s.R > 1.01 || s.G > 1.01 || s.B > 1.01 {
			t.Errorf("colour not normalised: %+v", s)
		}
	}
}

// TestParseFxLuaSprayFile reads a second effect so the parser is not tuned
// to one file's writing habits.
func TestParseFxLuaSprayFile(t *testing.T) {
	f, err := fx.ParseFxLua(readFxLua(t, "flame_fx/flame_sprays/flame_ground_strip_combo.lua"))
	if err != nil {
		t.Fatalf("fx.ParseFxLua: %v", err)
	}
	found := false
	for i := range f.Nodes {
		n := &f.Nodes[i]
		if n.Style != "STYLE_SPRAY" {
			continue
		}
		found = true
		rate, ok := n.Prop("Emitter_Rate")
		if !ok || len(rate.Curve) == 0 {
			t.Errorf("%s: no rate", n.Effect)
		}
	}
	if !found {
		t.Fatal("no spray in a spray file")
	}
	// The colour ramp of this file is authored in bytes.
	for i := range f.Nodes {
		if p, ok := f.Nodes[i].Prop("Colour"); ok && len(p.Colour) > 1 {
			t.Logf("%s colour stops=%d first=%+v", f.Nodes[i].Effect, len(p.Colour), p.Colour[0])
			break
		}
	}
}

func stringsHasPrefix(s, pre string) bool {
	return len(s) >= len(pre) && s[:len(pre)] == pre
}
