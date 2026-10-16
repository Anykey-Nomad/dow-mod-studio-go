package main

import (
	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/raster"
	"dow-mod-studio/internal/whm"
	"fmt"
	"image"
	"log"
	"path/filepath"
	"strings"
	"time"

	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget/material"
)

const whmGroundTexture = unitGroundTexture

// whmMeasurePass identifies the scroll panel's synthetic measurement height.
// Flex consumes the fixed rows before giving the viewport to this child, so the
// incoming maximum is slightly below panelScrollMeasureHeight rather than equal.
func whmMeasurePass(maxHeight int) bool {
	return maxHeight >= panelScrollMeasureHeight/2
}

// whmViewerLayout keeps scroll-panel measurement from starting a renderer with
// the panel's synthetic 1<<20px height. During the actual layout pass the panel
// gives the viewer its real viewport height and rendering starts normally.
func whmViewerLayout(ui *uiState, t *fileTab) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		if whmMeasurePass(gtx.Constraints.Max.Y) {
			return layout.Dimensions{Size: image.Pt(gtx.Constraints.Max.X, 0)}
		}
		gtx.Constraints.Min.Y = 0
		return ui.layoutWhmModelViewport(gtx, t)
	}
}

// layoutWhmModelViewport builds the .whm preview the way the Object window's
// Viewport section builds its view area: one framed panel with the section
// header, the canvas taking the space beside a side panel of the width the
// viewport's side panel takes, and the lists in that side panel. The playback
// strip stays under the canvas rather than moving into the side panel: the
// viewport's own side controls are drawn as a column, while this strip is one
// wide row of buttons that does not fit in a 300dp panel.
func (ui *uiState) layoutWhmModelViewport(gtx layout.Context, t *fileTab) layout.Dimensions {
	if t == nil || t.model == nil || t.model.model == nil {
		return ui.layoutModelViewer(gtx, t)
	}
	v := t.model
	v.sideControls = true
	sideW := gtx.Dp(unit.Dp(unitViewportSideDp))
	return panelFrame(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.layoutUnitColumnHeader(gtx, "Viewport")
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, cHeaderLine, 1) }),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(6), Left: unit.Dp(6), Right: unit.Dp(6), Bottom: unit.Dp(6)}.Layout(gtx,
					func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
							layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
								return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
									layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
										return ui.layoutModelViewer(gtx, t)
									}),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return ui.modelControlsWithUV(gtx, t)
									}),
								)
							}),
							layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
							layout.Rigid(ui.fixedPanel(sideW, func(gtx layout.Context) layout.Dimensions {
								return ui.layoutWhmModelList(gtx, t)
							})),
						)
					})
			}),
		)
	})
}

func (ui *uiState) layoutWhmModelList(gtx layout.Context, t *fileTab) layout.Dimensions {
	return panelFrame(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.whmModelListControls(gtx, t) }),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return ui.panelScrollView(gtx, &t.modelListScroll, func(gtx layout.Context) layout.Dimensions {
					return ui.whmModelList(gtx, t)
				})
			}),
		)
	})
}

// whmModelListControls switches the list beside the canvas.
func (ui *uiState) whmModelListControls(gtx layout.Context, t *fileTab) layout.Dimensions {
	labels := []string{"Animations", "Markers", "Bones", "FX"}
	if t.modelListMode < 0 || t.modelListMode >= len(labels) {
		t.modelListMode = 0
	}
	for i := range labels {
		if t.modelListClicks[i].Clicked(gtx) {
			t.modelListMode = i
			t.modelListScroll = panelScrollState{}
		}
	}
	return ui.modelListTabButtons(gtx, labels, t.modelListClicks[:], t.modelListMode)
}

func (ui *uiState) whmModelList(gtx layout.Context, t *fileTab) layout.Dimensions {
	if t.modelListMode == 3 {
		if t.model == nil || t.model.model == nil {
			return centeredPanelLabel(gtx, ui.th, "No model", unit.Sp(10), cTextDim)
		}
		return ui.unitFXListWithHeader(gtx, t.model, &t.modelFXClicks)
	}
	return ui.unitModelListContent(gtx, t, t.modelListMode)
}

func loadWhmGroundImage(tree *modtree.ModTree) image.Image {
	if tree == nil {
		return nil
	}
	data, err := tree.ReadFile(whmGroundTexture)
	if err != nil {
		if file := tree.FindMaterialByName(filepath.Base(whmGroundTexture)); file != nil {
			data, err = tree.ReadTreeFile(file)
		}
	}
	if err != nil {
		return nil
	}
	img, _, err := raster.DecodeImageData(data, strings.TrimPrefix(filepath.Ext(whmGroundTexture), "."))
	if err != nil {
		return nil
	}
	return img
}

// .whm viewer. Dawn of War ships models as .whm; the Object Editor consumes the
// same geometry in an .sgm container. whm.ConvertToSgm does that rewrite in
// process, so no Blender round trip is needed.

func (ui *uiState) openWhm(virtualPath string) {
	if ui.modTree == nil {
		return
	}
	if idx := ui.findTabIndex(virtualPath); idx >= 0 {
		ui.tabSel = idx + 1
		ui.ensureTabClicks()
		ui.statusText = fmt.Sprintf("Opened %s", filepath.Base(virtualPath))
		return
	}
	t := &fileTab{path: virtualPath, kind: "whm"}
	data, err := ui.modTree.ReadFile(virtualPath)
	if err != nil {
		t.whmError = err.Error()
	} else {
		t.whmBytes = len(data)
		info, err := whm.Inspect(data)
		if err != nil {
			t.whmError = err.Error()
		} else {
			t.whmInfo = info
		}
		// The 3D preview is a bonus: a model the converter cannot handle is
		// still worth looking at, so a parse failure only hides the canvas.
		parseStart := time.Now()
		model, perr := ParseWhmModel(data)
		if perr != nil {
			log.Printf("3D preview: parse %s failed after %v: %v", filepath.Base(virtualPath), time.Since(parseStart), perr)
		} else {
			ground := loadWhmGroundImage(ui.modTree)
			t.model = newObjectModelViewer(model, virtualPath, ground)
			t.model.sideControls = true
			log.Printf("3D preview: parsed %s in %v: %d verts, %d meshes, %d bones, %d anims",
				filepath.Base(virtualPath), time.Since(parseStart),
				len(model.Vertices), len(model.Meshes), len(model.Bones), len(model.Animations))
		}
	}
	ui.openTabs = append(ui.openTabs, t)
	ui.tabSel = len(ui.openTabs)
	ui.ensureTabClicks()
	if t.whmError != "" {
		ui.statusText = "Open " + filepath.Base(virtualPath) + " failed: " + t.whmError
	} else {
		ui.statusText = "Opened " + filepath.Base(virtualPath)
	}
}

// whmTargetSGM maps a .whm virtual path onto the .sgm that will be produced
// next to it inside the current mod's ModFolder/Data tree.
func whmTargetSGM(virtualPath string) string {
	return strings.TrimSuffix(virtualPath, filepath.Ext(virtualPath)) + ".sgm"
}

// convertWhmToSgmFile rewrites the open .whm as an Object Editor .sgm and
// writes it into the current mod folder. WriteOverlayFile resolves the virtual
// path against the mod's writable ModFolder/Data, so a model coming from a
// parent mod or from an SGA archive still lands in this mod.
func (ui *uiState) convertWhmToSgmFile(t *fileTab) {
	if t == nil || t.kind != "whm" || t.whmConverting || t.whmError != "" {
		return
	}
	if ui.modTree == nil {
		ui.statusText = "Convert — no mod loaded"
		return
	}
	t.whmConverting = true
	t.whmExportDone = ""
	target := whmTargetSGM(t.path)

	// Resolve the destination before doing the work so a bad mod folder fails
	// fast and no half written file is left behind.
	if _, err := ui.modTree.OverlayPath(target); err != nil {
		t.whmConverting = false
		ui.statusText = "Convert failed: " + err.Error()
		return
	}
	data, err := ui.modTree.ReadFile(t.path)
	if err != nil {
		t.whmConverting = false
		ui.statusText = "Convert failed: " + err.Error()
		return
	}
	sgm, info, err := whm.ConvertToSgm(data)
	if err != nil {
		t.whmConverting = false
		ui.statusText = "Convert failed: " + err.Error()
		return
	}
	diskPath, err := ui.modTree.WriteOverlayFile(target, sgm)
	if err != nil {
		t.whmConverting = false
		ui.statusText = "Convert failed: " + err.Error()
		return
	}
	t.whmConverting = false
	t.whmExportDone = diskPath
	if info != nil {
		t.whmInfo = info
	}
	ui.statusText = fmt.Sprintf("Converted %s → %s (%d bytes)", filepath.Base(t.path), diskPath, len(sgm))
	log.Printf("convertWhmToSgmFile: wrote %s (%d bytes)", diskPath, len(sgm))
	// Surface the new .sgm in the file tree without losing the open tabs.
	if err := ui.refreshModTreePreservingUI(); err != nil {
		log.Printf("convertWhmToSgmFile: tree refresh failed: %v", err)
	}
}

func (ui *uiState) layoutWhmViewer(gtx layout.Context, t *fileTab) layout.Dimensions {
	if t == nil {
		return layout.Dimensions{}
	}
	if t.whmError != "" {
		if t.model != nil {
			// Conversion failed but the geometry parsed, so keep the preview.
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					l := material.Label(ui.th, unit.Sp(10), "Conversion unsupported: "+t.whmError)
					l.Color = rgb(0xB06000)
					return layout.Inset{Top: unit.Dp(6), Left: unit.Dp(8)}.Layout(gtx, l.Layout)
				}),
				layout.Flexed(1, whmViewerLayout(ui, t)),
			)
		}
		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.th, unit.Sp(11), "Failed: "+t.whmError)
			l.Color = rgb(0xB00020)
			return l.Layout(gtx)
		})
	}
	convertLabel := "Convert to SGM"
	switch {
	case t.whmConverting:
		convertLabel = "Converting…"
	case t.whmExportDone != "":
		convertLabel = "SGM created"
	}
	body := []layout.FlexChild{
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return ui.smallButtonDisabled(gtx, &t.whmConvertClick, convertLabel, t.whmConverting)
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					l := material.Label(ui.th, unit.Sp(9), "Object Editor model for "+t.path)
					l.Color = cTextDim
					return l.Layout(gtx)
				}),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{} }),
			)
		}),
	}
	if t.model != nil {
		// Reuse the Object Viewport's viewer strip and model facts. This keeps
		// both entry points on one playback, camera and rendering implementation.
		body = append(body,
			layout.Rigid(layout.Spacer{Height: unit.Dp(6)}.Layout),
			layout.Flexed(1, whmViewerLayout(ui, t)),
		)
	} else {
		body = append(body,
			layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.whmInfoTable(gtx, t) }),
		)
	}
	body = append(body, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		if t.whmExportDone == "" {
			return layout.Dimensions{}
		}
		l := material.Label(ui.th, unit.Sp(9), "SGM: "+t.whmExportDone)
		l.Color = rgb(0x2E7D32)
		return layout.Inset{Top: unit.Dp(6)}.Layout(gtx, l.Layout)
	}))
	return layout.Inset{Top: unit.Dp(8), Left: unit.Dp(8), Right: unit.Dp(8), Bottom: unit.Dp(8)}.Layout(gtx,
		func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx, body...)
		})
}

func (ui *uiState) whmInfoTable(gtx layout.Context, t *fileTab) layout.Dimensions {
	if t == nil {
		return layout.Dimensions{}
	}
	if t.whmInfo == nil {
		if t.whmError == "" {
			return centeredPanelLabel(gtx, ui.th, "No model data", unit.Sp(10), cTextDim)
		}
		return layout.Dimensions{}
	}
	info := t.whmInfo
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.tableHead(gtx, "Name", "Value") }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.tableRow2(gtx, "Name", filepath.Base(t.path), false)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.tableRow2(gtx, "Model", orDash(info.Name), true)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.tableRow2(gtx, "Size", fmt.Sprintf("%d bytes", t.whmBytes), false)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.tableRow2(gtx, "Bones", fmt.Sprintf("%d", info.Bones), true)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.tableRow2(gtx, "Meshes", fmt.Sprintf("%d", info.Meshes), false)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.tableRow2(gtx, "Vertices", fmt.Sprintf("%d", info.Vertices), true)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.tableRow2(gtx, "Materials", fmt.Sprintf("%d", info.Materials), false)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.tableRow2(gtx, "Textures", fmt.Sprintf("%d", info.Textures), true)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.tableRow2(gtx, "Markers", fmt.Sprintf("%d", info.Markers), false)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.tableRow2(gtx, "Cameras", fmt.Sprintf("%d", info.Cameras), true)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.tableRow2(gtx, "Animations", fmt.Sprintf("%d", info.Animations), false)
		}),
	)
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}
