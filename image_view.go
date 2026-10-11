package main

import (
	"context"
	"dow-mod-studio/internal/modtree"
	"fmt"
	"image"
	"path/filepath"
	"strings"
	"time"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget/material"
)

type imageDecodeResult struct {
	data    image.Image
	mipmaps []image.Image
	format  string
	bytes   int
	err     error
}

const (
	imageCacheMaxBytes   int64 = 256 << 20
	imageCacheMaxEntries       = 16
)

type imageCacheEntry struct {
	data     image.Image
	mipmaps  []image.Image
	format   string
	bytes    int
	cachedAt uint64
}

func imageCacheKey(path string) string {
	return strings.ToLower(filepath.Clean(path))
}

func (ui *uiState) cachedImage(path string) (imageCacheEntry, bool) {
	if ui.imageCache == nil {
		return imageCacheEntry{}, false
	}
	key := imageCacheKey(path)
	entry, ok := ui.imageCache[key]
	if !ok {
		return imageCacheEntry{}, false
	}
	ui.imageCacheSeq++
	entry.cachedAt = ui.imageCacheSeq
	ui.imageCache[key] = entry
	return entry, true
}

func (ui *uiState) cacheDecodedImage(path string, data image.Image, mipmaps []image.Image, format string, bytes int) {
	if data == nil {
		return
	}
	if ui.imageCache == nil {
		ui.imageCache = make(map[string]imageCacheEntry)
	}
	cacheImages := mipmaps
	if len(cacheImages) == 0 {
		cacheImages = []image.Image{data}
	}
	cost := imageImagesCost(cacheImages)
	if cost > imageCacheMaxBytes {
		return
	}
	key := imageCacheKey(path)
	if old, ok := ui.imageCache[key]; ok {
		ui.imageCacheBytes -= imageCacheEntryCost(old)
	}
	ui.imageCacheSeq++
	ui.imageCache[key] = imageCacheEntry{
		data:     data,
		mipmaps:  append([]image.Image(nil), mipmaps...),
		format:   strings.ToUpper(format),
		bytes:    bytes,
		cachedAt: ui.imageCacheSeq,
	}
	ui.imageCacheBytes += cost
	for len(ui.imageCache) > imageCacheMaxEntries || ui.imageCacheBytes > imageCacheMaxBytes {
		var oldestKey string
		var oldestAt uint64
		for k, entry := range ui.imageCache {
			if oldestKey == "" || entry.cachedAt < oldestAt {
				oldestKey, oldestAt = k, entry.cachedAt
			}
		}
		if oldestKey == "" {
			break
		}
		ui.imageCacheBytes -= imageCacheEntryCost(ui.imageCache[oldestKey])
		delete(ui.imageCache, oldestKey)
	}
}

func imageCacheEntryCost(entry imageCacheEntry) int64 {
	if len(entry.mipmaps) > 0 {
		return imageImagesCost(entry.mipmaps)
	}
	return imageImagesCost([]image.Image{entry.data})
}

func imageImagesCost(images []image.Image) int64 {
	var total int64
	for _, img := range images {
		if img == nil {
			continue
		}
		b := img.Bounds()
		total += int64(b.Dx()) * int64(b.Dy()) * 4
	}
	return total
}

func (ui *uiState) clearImageCache() {
	ui.imageCache = nil
	ui.imageCacheBytes = 0
	ui.imageCacheSeq = 0
}

func (ui *uiState) openImage(virtualPath string) {
	if ui.modTree == nil {
		return
	}
	if idx := ui.findTabIndex(virtualPath); idx >= 0 {
		ui.tabSel = idx + 1
		ui.ensureTabClicks()
		ui.statusText = fmt.Sprintf("Opened %s", filepath.Base(virtualPath))
		return
	}
	format := strings.TrimPrefix(strings.ToLower(filepath.Ext(virtualPath)), ".")
	if entry, ok := ui.cachedImage(virtualPath); ok {
		t := &fileTab{
			path:          virtualPath,
			kind:          "image",
			imageData:     entry.data,
			imageMipmaps:  append([]image.Image(nil), entry.mipmaps...),
			imageMipLevel: 0,
			imageBytes:    entry.bytes,
			imageFormat:   entry.format,
			imageFit:      false,
			imageZoom:     1,
		}
		ui.openTabs = append(ui.openTabs, t)
		ui.tabSel = len(ui.openTabs)
		ui.ensureTabClicks()
		ui.statusText = fmt.Sprintf("Opened %s — cached", filepath.Base(virtualPath))
		return
	}
	t := &fileTab{
		path:         virtualPath,
		kind:         "image",
		imageFormat:  strings.ToUpper(format),
		imageFit:     false,
		imageZoom:    1,
		imageLoading: true,
	}
	ui.openTabs = append(ui.openTabs, t)
	ui.tabSel = len(ui.openTabs)
	ui.ensureTabClicks()
	ui.startImageDecode(t, ui.modTree)
	ui.statusText = fmt.Sprintf("Loading %s…", filepath.Base(virtualPath))
}

func (ui *uiState) startImageDecode(t *fileTab, tree *modtree.ModTree) {
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan imageDecodeResult, 1)
	t.imageDecodeStop = cancel
	t.imageDecodeCh = ch
	format := strings.TrimPrefix(strings.ToLower(filepath.Ext(t.path)), ".")
	path := t.path
	go func() {
		result := imageDecodeResult{format: strings.ToUpper(format)}
		data, err := tree.ReadFile(path)
		result.bytes = len(data)
		if err == nil {
			result.data, result.mipmaps, result.format, err = decodeImageDataWithMips(data, format)
		}
		result.err = err
		select {
		case ch <- result:
		case <-ctx.Done():
		}
	}()
}

// pollImageDecodes is called on the UI goroutine. Workers only send results;
// all tab state is changed here, avoiding races with Gio layout code.
func (ui *uiState) pollImageDecodes() {
	for _, t := range ui.openTabs {
		if t.imageDecodeCh == nil {
			continue
		}
		select {
		case result := <-t.imageDecodeCh:
			t.imageDecodeCh = nil
			if t.imageDecodeStop != nil {
				t.imageDecodeStop()
				t.imageDecodeStop = nil
			}
			t.imageLoading = false
			t.imageBytes = result.bytes
			t.imageFormat = strings.ToUpper(result.format)
			t.imageData = result.data
			t.imageMipmaps = result.mipmaps
			t.imageMipLevel = 0
			if result.err != nil {
				t.imageError = result.err.Error()
				if ui.activeTab() == t {
					ui.statusText = fmt.Sprintf("Failed to open %s: %s", filepath.Base(t.path), t.imageError)
				}
			} else {
				ui.cacheDecodedImage(t.path, t.imageData, t.imageMipmaps, t.imageFormat, t.imageBytes)
				if ui.activeTab() == t {
					b := t.imageData.Bounds()
					ui.statusText = fmt.Sprintf("Opened %s — %s %dx%d", filepath.Base(t.path), t.imageFormat, b.Dx(), b.Dy())
				}
			}
		default:
		}
	}
}

func (ui *uiState) selectImageMip(t *fileTab, delta int) {
	if t == nil || len(t.imageMipmaps) < 2 {
		return
	}
	t.imageMipLevel += delta
	if t.imageMipLevel < 0 {
		t.imageMipLevel = 0
	}
	if t.imageMipLevel >= len(t.imageMipmaps) {
		t.imageMipLevel = len(t.imageMipmaps) - 1
	}
	t.imageData = t.imageMipmaps[t.imageMipLevel]
}

func (ui *uiState) layoutImageViewer(gtx layout.Context, t *fileTab) layout.Dimensions {
	if t == nil {
		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.th, unit.Sp(11), "No image selected")
			l.Color = cTextDim
			return l.Layout(gtx)
		})
	}
	if t.imageLoading {
		gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(100 * time.Millisecond)})
		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					l := material.Label(ui.th, unit.Sp(12), "Loading image…")
					l.Color = cTextDim
					return l.Layout(gtx)
				}),
				layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					l := material.Label(ui.th, unit.Sp(10), filepath.Base(t.path))
					l.Color = cTextDim
					return l.Layout(gtx)
				}),
			)
		})
	}
	if t.imageError != "" || t.imageData == nil {
		msg := t.imageError
		if msg == "" {
			msg = "No image data"
		}
		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.th, unit.Sp(11), "Failed: "+msg)
			l.Color = rgb(0xB00020)
			return l.Layout(gtx)
		})
	}
	if t.imageMipPrev.Clicked(gtx) {
		ui.selectImageMip(t, -1)
	}
	if t.imageMipNext.Clicked(gtx) {
		ui.selectImageMip(t, 1)
	}
	if t.imageZoomIn.Clicked(gtx) {
		t.imageFit = false
		if t.imageZoom < 8 {
			t.imageZoom *= 1.25
		}
	}
	if t.imageZoomOut.Clicked(gtx) {
		t.imageFit = false
		if t.imageZoom > 0.1 {
			t.imageZoom /= 1.25
		}
	}
	if t.imageFitClick.Clicked(gtx) {
		t.imageFit = true
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Left: unit.Dp(6), Right: unit.Dp(6), Top: unit.Dp(4), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.smallButton(gtx, &t.imageZoomOut, "−") }),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						zoom := fmt.Sprintf("%.0f%%", t.imageZoom*100)
						if t.imageFit {
							zoom = "Fit"
						}
						l := material.Label(ui.th, unit.Sp(10), zoom)
						l.Color = cTextDim
						return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, l.Layout)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.smallButton(gtx, &t.imageZoomIn, "+") }),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.smallButton(gtx, &t.imageFitClick, "Fit") }),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						if len(t.imageMipmaps) < 2 {
							return layout.Dimensions{}
						}
						return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.smallButton(gtx, &t.imageMipPrev, "◀") }),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								l := material.Label(ui.th, unit.Sp(10), fmt.Sprintf("Mip %d/%d", t.imageMipLevel+1, len(t.imageMipmaps)))
								l.Color = cTextDim
								return layout.Inset{Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx, l.Layout)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.smallButton(gtx, &t.imageMipNext, "▶") }),
						)
					}),
					layout.Flexed(1, rowGap()), // takes the leftover width and none of the height, or the row would eat the canvas below it
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						b := t.imageData.Bounds()
						l := material.Label(ui.th, unit.Sp(10), fmt.Sprintf("%s  %d × %d", t.imageFormat, b.Dx(), b.Dy()))
						l.Color = cTextDim
						return l.Layout(gtx)
					}),
				)
			})
		}),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return ui.drawImageCanvas(gtx, t) }),
	)
}

func (ui *uiState) drawImageCanvas(gtx layout.Context, t *fileTab) layout.Dimensions {
	area := gtx.Constraints.Max
	if area.X < 1 || area.Y < 1 {
		return layout.Dimensions{}
	}
	gtx.Constraints.Min = area
	b := t.imageData.Bounds()
	scale := t.imageZoom
	if t.imageFit {
		scale = float32(area.X) / float32(b.Dx())
		y := float32(area.Y) / float32(b.Dy())
		if y < scale {
			scale = y
		}
		if scale <= 0 {
			scale = 1
		}
	}
	dw := int(float32(b.Dx())*scale + 0.5)
	dh := int(float32(b.Dy())*scale + 0.5)
	x := (area.X - dw) / 2
	y := (area.Y - dh) / 2
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			st := clip.Rect{Max: area}.Push(gtx.Ops)
			paint.ColorOp{Color: rgb(0x303030)}.Add(gtx.Ops)
			paint.PaintOp{}.Add(gtx.Ops)
			st.Pop()
			return layout.Dimensions{Size: area}
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			st := clip.Rect{Max: area}.Push(gtx.Ops)
			defer st.Pop()
			aff := op.Affine(f32.AffineId().Scale(f32.Pt(0, 0), f32.Pt(scale, scale)).Offset(f32.Pt(float32(x)-float32(b.Min.X)*scale, float32(y)-float32(b.Min.Y)*scale)))
			tr := aff.Push(gtx.Ops)
			paint.NewImageOp(t.imageData).Add(gtx.Ops)
			paint.PaintOp{}.Add(gtx.Ops)
			tr.Pop()
			return layout.Dimensions{Size: area}
		}),
	)
}

func (ui *uiState) imageInfoTable(gtx layout.Context, t *fileTab) layout.Dimensions {
	if t == nil {
		return layout.Dimensions{}
	}
	b := image.Rectangle{}
	if t.imageData != nil {
		b = t.imageData.Bounds()
	}
	format := t.imageFormat
	if format == "" {
		format = "—"
	}
	rows := [][2]string{
		{"Name", filepath.Base(t.path)},
		{"Format", format},
		{"Dimensions", fmt.Sprintf("%d × %d", b.Dx(), b.Dy())},
		{"File size", fmt.Sprintf("%d bytes", t.imageBytes)},
		{"Source", "Merged mod tree"},
	}
	if len(t.imageMipmaps) > 1 {
		rows = append(rows, [2]string{"Mip level", fmt.Sprintf("%d / %d", t.imageMipLevel+1, len(t.imageMipmaps))})
	}
	children := make([]layout.FlexChild, 0, len(rows)+1)
	children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.tableHead(gtx, "Name", "Value") }))
	for i, row := range rows {
		i, row := i, row
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.tableRow2(gtx, row[0], row[1], i%2 == 1) }))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}
