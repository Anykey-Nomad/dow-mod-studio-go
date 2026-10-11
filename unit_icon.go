package main

import (
	"dow-mod-studio/internal/rgd"
	"image"
	"strings"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
)

// ── object icons ────────────────────────────────────────────────────────────
//
// The game gives every unit and building an icon: a .tga under
// Data\art\ui\ingame\<race>_icons, named after the unit's own file
// (necron_lord → necron_icons/necron_lord_icon.tga). The name the unit's .rgd
// records in ui_ext\ui_info\icon_name is the same file with the race folder in
// front of it, so the file-name convention resolves nearly every icon without
// reading the unit at all, and the recorded name is the fallback for the ones
// that do not follow it.
//
// Icons are decoded once and kept for as long as the mod is open, keyed by the
// unit's tree path: the dropdown asks for the icons of its visible rows on
// every frame, and decoding a .tga per frame is not that.

// iconCacheEntry is one unit's icon lookup. A miss is cached as well — img is
// nil and tried says the lookup was made — so a unit with no icon is not
// searched for again on every frame.
type iconCacheEntry struct {
	img   image.Image
	tried bool
}

// unitIcon returns the decoded icon of a unit or building, or nil when the mod
// has none.
func (ui *uiState) unitIcon(u unitEntry) image.Image {
	if ui.modTree == nil || u.path == "" {
		return nil
	}
	mod := ui.modTree.FileMapName
	if ui.unitIcons == nil || ui.unitIconsMod != mod {
		ui.unitIcons = make(map[string]iconCacheEntry)
		ui.unitIconsMod = mod
	}
	if e, ok := ui.unitIcons[u.path]; ok {
		return e.img
	}
	img := ui.loadUnitIcon(u)
	ui.unitIcons[u.path] = iconCacheEntry{img: img, tried: true}
	return img
}

// loadUnitIcon resolves and decodes the icon of one unit, cheapest lookup
// first: the game's naming convention, then the file anywhere in the tree,
// then the name the unit's own .rgd records — reading the unit is the
// expensive part, so it is last.
func (ui *uiState) loadUnitIcon(u unitEntry) image.Image {
	if ui.modTree == nil {
		return nil
	}
	stem := unitTemplateIconBase(u.race) + `\` + u.name + "_icon"
	if img := ui.readIconAt(`art\ui\ingame\` + stem); img != nil {
		return img
	}
	if img := ui.readIconByName(u.name + "_icon"); img != nil {
		return img
	}
	data, err := ui.modTree.ReadFile(u.path)
	if err != nil {
		return nil
	}
	rf, err := rgd.ParseRgd(data)
	if err != nil {
		return nil
	}
	icon := rgdEntryStr(rgdFindPath(rf.Root, "ui_ext", "ui_info", "icon_name"))
	icon = strings.TrimSpace(icon)
	if icon == "" {
		return nil
	}
	icon = strings.ReplaceAll(icon, "/", `\`)
	if img := ui.readIconAt(`art\ui\ingame\` + icon); img != nil {
		return img
	}
	// icon_name carries the race folder in front ("necrons/necron_icons/...")
	// while the file sits directly under the icons folder.
	if i := strings.Index(icon, `\`); i >= 0 {
		if img := ui.readIconAt(`art\ui\ingame\` + icon[i+1:]); img != nil {
			return img
		}
	}
	if i := strings.LastIndex(icon, `\`); i >= 0 {
		icon = icon[i+1:]
	}
	return ui.readIconByName(icon)
}

// readIconAt looks for the given tree path without an extension, trying the
// two forms an icon ships in.
func (ui *uiState) readIconAt(virtualPathNoExt string) image.Image {
	for _, ext := range []string{".tga", ".rsh"} {
		data, err := ui.modTree.ReadFile(virtualPathNoExt + ext)
		if err != nil {
			continue
		}
		if img := decodeIconData(ext, data); img != nil {
			return img
		}
	}
	return nil
}

// readIconByName finds an icon file by its base name anywhere in the tree,
// which covers a mod that filed its icons somewhere of its own.
func (ui *uiState) readIconByName(stem string) image.Image {
	for _, ext := range []string{".tga", ".rsh"} {
		f := ui.modTree.FindByName(stem + ext)
		if f == nil {
			continue
		}
		data, err := ui.modTree.ReadFile(f.FullPath)
		if err != nil {
			continue
		}
		if img := decodeIconData(ext, data); img != nil {
			return img
		}
	}
	return nil
}

// decodeIconData decodes one icon file. Both forms are ordinary images; a file
// that will not decode simply yields no icon rather than an error line in a
// list that is about names.
func decodeIconData(ext string, data []byte) image.Image {
	switch ext {
	case ".tga":
		if img, err := decodeTGA(data); err == nil {
			return img
		}
	case ".rsh":
		if img, _, err := decodeRshImage(data, 0); err == nil {
			return img
		}
	}
	return nil
}

// unitIconLayout paints img fitted inside a w×h box, preserving its aspect and
// centring it, and reserves the box even when there is no icon — a column of
// rows keeps its text aligned whether or not the mod ships pictures.
func unitIconLayout(gtx layout.Context, img image.Image, w, h unit.Dp) layout.Dimensions {
	boxW, boxH := gtx.Dp(w), gtx.Dp(h)
	if boxW < 1 || boxH < 1 {
		return layout.Dimensions{}
	}
	size := image.Pt(boxW, boxH)
	if img == nil {
		return layout.Dimensions{Size: size}
	}
	b := img.Bounds()
	if b.Dx() < 1 || b.Dy() < 1 {
		return layout.Dimensions{Size: size}
	}
	scale := float32(boxW) / float32(b.Dx())
	if s := float32(boxH) / float32(b.Dy()); s < scale {
		scale = s
	}
	dw := int(float32(b.Dx())*scale + 0.5)
	dh := int(float32(b.Dy())*scale + 0.5)
	x := (boxW - dw) / 2
	y := (boxH - dh) / 2
	st := clip.Rect{Max: size}.Push(gtx.Ops)
	aff := op.Affine(f32.AffineId().Scale(f32.Pt(0, 0), f32.Pt(scale, scale)).Offset(f32.Pt(
		float32(x)-float32(b.Min.X)*scale,
		float32(y)-float32(b.Min.Y)*scale,
	)))
	tr := aff.Push(gtx.Ops)
	paint.NewImageOp(img).Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	tr.Pop()
	st.Pop()
	return layout.Dimensions{Size: size}
}
