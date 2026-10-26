package app

import (
	"context"
	"encoding/binary"
	"fmt"
	"image"
	"io"
	"path/filepath"
	"strings"
	"time"

	"dow-mod-studio/internal/audioplayer"
	"dow-mod-studio/internal/fda"
	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/raster"
	"dow-mod-studio/internal/wav"
	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
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

func CachedImage(ui *uiState, path string) (imageCacheEntry, bool) {
	if ui.ImageCache == nil {
		return imageCacheEntry{}, false
	}
	key := imageCacheKey(path)
	entry, ok := ui.ImageCache[key]
	if !ok {
		return imageCacheEntry{}, false
	}
	ui.ImageCacheSeq++
	entry.cachedAt = ui.ImageCacheSeq
	ui.ImageCache[key] = entry
	return entry, true
}

func CacheDecodedImage(ui *uiState, path string, data image.Image, mipmaps []image.Image, format string, bytes int) {
	if data == nil {
		return
	}
	if ui.ImageCache == nil {
		ui.ImageCache = make(map[string]imageCacheEntry)
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
	if old, ok := ui.ImageCache[key]; ok {
		ui.ImageCacheBytes -= imageCacheEntryCost(old)
	}
	ui.ImageCacheSeq++
	ui.ImageCache[key] = imageCacheEntry{
		data:     data,
		mipmaps:  append([]image.Image(nil), mipmaps...),
		format:   strings.ToUpper(format),
		bytes:    bytes,
		cachedAt: ui.ImageCacheSeq,
	}
	ui.ImageCacheBytes += cost
	for len(ui.ImageCache) > imageCacheMaxEntries || ui.ImageCacheBytes > imageCacheMaxBytes {
		var oldestKey string
		var oldestAt uint64
		for k, entry := range ui.ImageCache {
			if oldestKey == "" || entry.cachedAt < oldestAt {
				oldestKey, oldestAt = k, entry.cachedAt
			}
		}
		if oldestKey == "" {
			break
		}
		ui.ImageCacheBytes -= imageCacheEntryCost(ui.ImageCache[oldestKey])
		delete(ui.ImageCache, oldestKey)
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

func ClearImageCache(ui *uiState) {
	ui.ImageCache = nil
	ui.ImageCacheBytes = 0
	ui.ImageCacheSeq = 0
}

func OpenImage(ui *uiState, virtualPath string) {
	if ui.ModTree == nil {
		return
	}
	if idx := FindTabIndex(ui, virtualPath); idx >= 0 {
		ui.TabSel = idx + 1
		EnsureTabClicks(ui)
		ui.StatusText = fmt.Sprintf("Opened %s", filepath.Base(virtualPath))
		return
	}
	format := strings.TrimPrefix(strings.ToLower(filepath.Ext(virtualPath)), ".")
	if entry, ok := CachedImage(ui, virtualPath); ok {
		t := &fileTab{
			Path:          virtualPath,
			Kind:          "image",
			ImageData:     entry.data,
			ImageMipmaps:  append([]image.Image(nil), entry.mipmaps...),
			ImageMipLevel: 0,
			ImageBytes:    entry.bytes,
			ImageFormat:   entry.format,
			ImageFit:      false,
			ImageZoom:     1,
		}
		ui.OpenTabs = append(ui.OpenTabs, t)
		ui.TabSel = len(ui.OpenTabs)
		EnsureTabClicks(ui)
		ui.StatusText = fmt.Sprintf("Opened %s — cached", filepath.Base(virtualPath))
		return
	}
	t := &fileTab{
		Path:         virtualPath,
		Kind:         "image",
		ImageFormat:  strings.ToUpper(format),
		ImageFit:     false,
		ImageZoom:    1,
		ImageLoading: true,
	}
	ui.OpenTabs = append(ui.OpenTabs, t)
	ui.TabSel = len(ui.OpenTabs)
	EnsureTabClicks(ui)
	StartImageDecode(ui, t, ui.ModTree)
	ui.StatusText = fmt.Sprintf("Loading %s…", filepath.Base(virtualPath))
}

func StartImageDecode(ui *uiState, t *fileTab, tree *modtree.ModTree) {
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan imageDecodeResult, 1)
	t.ImageDecodeStop = cancel
	t.ImageDecodeCh = ch
	format := strings.TrimPrefix(strings.ToLower(filepath.Ext(t.Path)), ".")
	path := t.Path
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
func PollImageDecodes(ui *uiState) {
	for _, t := range ui.OpenTabs {
		if t.ImageDecodeCh == nil {
			continue
		}
		select {
		case result := <-t.ImageDecodeCh:
			t.ImageDecodeCh = nil
			if t.ImageDecodeStop != nil {
				t.ImageDecodeStop()
				t.ImageDecodeStop = nil
			}
			t.ImageLoading = false
			t.ImageBytes = result.bytes
			t.ImageFormat = strings.ToUpper(result.format)
			t.ImageData = result.data
			t.ImageMipmaps = result.mipmaps
			t.ImageMipLevel = 0
			if result.err != nil {
				t.ImageError = result.err.Error()
				if ActiveTab(ui) == t {
					ui.StatusText = fmt.Sprintf("Failed to open %s: %s", filepath.Base(t.Path), t.ImageError)
				}
			} else {
				CacheDecodedImage(ui, t.Path, t.ImageData, t.ImageMipmaps, t.ImageFormat, t.ImageBytes)
				if ActiveTab(ui) == t {
					b := t.ImageData.Bounds()
					ui.StatusText = fmt.Sprintf("Opened %s — %s %dx%d", filepath.Base(t.Path), t.ImageFormat, b.Dx(), b.Dy())
				}
			}
		default:
		}
	}
}

func SelectImageMip(ui *uiState, t *fileTab, delta int) {
	if t == nil || len(t.ImageMipmaps) < 2 {
		return
	}
	t.ImageMipLevel += delta
	if t.ImageMipLevel < 0 {
		t.ImageMipLevel = 0
	}
	if t.ImageMipLevel >= len(t.ImageMipmaps) {
		t.ImageMipLevel = len(t.ImageMipmaps) - 1
	}
	t.ImageData = t.ImageMipmaps[t.ImageMipLevel]
}

func LayoutImageViewer(ui *uiState, gtx layout.Context, t *fileTab) layout.Dimensions {
	if t == nil {
		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.Th, unit.Sp(11), "No image selected")
			l.Color = cTextDim
			return l.Layout(gtx)
		})
	}
	if t.ImageLoading {
		gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(100 * time.Millisecond)})
		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					l := material.Label(ui.Th, unit.Sp(12), "Loading image…")
					l.Color = cTextDim
					return l.Layout(gtx)
				}),
				layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					l := material.Label(ui.Th, unit.Sp(10), filepath.Base(t.Path))
					l.Color = cTextDim
					return l.Layout(gtx)
				}),
			)
		})
	}
	if t.ImageError != "" || t.ImageData == nil {
		msg := t.ImageError
		if msg == "" {
			msg = "No image data"
		}
		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.Th, unit.Sp(11), "Failed: "+msg)
			l.Color = rgb(0xB00020)
			return l.Layout(gtx)
		})
	}
	if t.ImageMipPrev.Clicked(gtx) {
		SelectImageMip(ui, t, -1)
	}
	if t.ImageMipNext.Clicked(gtx) {
		SelectImageMip(ui, t, 1)
	}
	if t.ImageZoomIn.Clicked(gtx) {
		t.ImageFit = false
		if t.ImageZoom < 8 {
			t.ImageZoom *= 1.25
		}
	}
	if t.ImageZoomOut.Clicked(gtx) {
		t.ImageFit = false
		if t.ImageZoom > 0.1 {
			t.ImageZoom /= 1.25
		}
	}
	if t.ImageFitClick.Clicked(gtx) {
		t.ImageFit = true
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Left: unit.Dp(6), Right: unit.Dp(6), Top: unit.Dp(4), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return SmallButton(ui, gtx, &t.ImageZoomOut, "−") }),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						zoom := fmt.Sprintf("%.0f%%", t.ImageZoom*100)
						if t.ImageFit {
							zoom = "Fit"
						}
						l := material.Label(ui.Th, unit.Sp(10), zoom)
						l.Color = cTextDim
						return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, l.Layout)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return SmallButton(ui, gtx, &t.ImageZoomIn, "+") }),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return SmallButton(ui, gtx, &t.ImageFitClick, "Fit") }),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						if len(t.ImageMipmaps) < 2 {
							return layout.Dimensions{}
						}
						return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions { return SmallButton(ui, gtx, &t.ImageMipPrev, "◀") }),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								l := material.Label(ui.Th, unit.Sp(10), fmt.Sprintf("Mip %d/%d", t.ImageMipLevel+1, len(t.ImageMipmaps)))
								l.Color = cTextDim
								return layout.Inset{Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx, l.Layout)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions { return SmallButton(ui, gtx, &t.ImageMipNext, "▶") }),
						)
					}),
					layout.Flexed(1, rowGap()), // takes the leftover width and none of the height, or the row would eat the canvas below it
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						b := t.ImageData.Bounds()
						l := material.Label(ui.Th, unit.Sp(10), fmt.Sprintf("%s  %d × %d", t.ImageFormat, b.Dx(), b.Dy()))
						l.Color = cTextDim
						return l.Layout(gtx)
					}),
				)
			})
		}),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return DrawImageCanvas(ui, gtx, t) }),
	)
}

func DrawImageCanvas(ui *uiState, gtx layout.Context, t *fileTab) layout.Dimensions {
	area := gtx.Constraints.Max
	if area.X < 1 || area.Y < 1 {
		return layout.Dimensions{}
	}
	gtx.Constraints.Min = area
	b := t.ImageData.Bounds()
	scale := t.ImageZoom
	if t.ImageFit {
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
			paint.NewImageOp(t.ImageData).Add(gtx.Ops)
			paint.PaintOp{}.Add(gtx.Ops)
			tr.Pop()
			return layout.Dimensions{Size: area}
		}),
	)
}

func ImageInfoTable(ui *uiState, gtx layout.Context, t *fileTab) layout.Dimensions {
	if t == nil {
		return layout.Dimensions{}
	}
	b := image.Rectangle{}
	if t.ImageData != nil {
		b = t.ImageData.Bounds()
	}
	format := t.ImageFormat
	if format == "" {
		format = "—"
	}
	rows := [][2]string{
		{"Name", filepath.Base(t.Path)},
		{"Format", format},
		{"Dimensions", fmt.Sprintf("%d × %d", b.Dx(), b.Dy())},
		{"File size", fmt.Sprintf("%d bytes", t.ImageBytes)},
		{"Source", "Merged mod tree"},
	}
	if len(t.ImageMipmaps) > 1 {
		rows = append(rows, [2]string{"Mip level", fmt.Sprintf("%d / %d", t.ImageMipLevel+1, len(t.ImageMipmaps))})
	}
	children := make([]layout.FlexChild, 0, len(rows)+1)
	children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions { return TableHead(ui, gtx, "Name", "Value") }))
	for i, row := range rows {
		i, row := i, row
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions { return TableRow2(ui, gtx, row[0], row[1], i%2 == 1) }))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}

// The image decoders (.tga, .dds, .rtx, .rsh and the BC7/DXT block codecs)
// live in internal/raster. This file keeps the names the app was calling,
// so the viewer, the FX overlay and the GPU renderers read the same calls
// they did before the move.

type RshImage = raster.RshImage
type RshMip = raster.RshMip

var imageViewerExtensions = raster.ImageViewerExtensions

func isImageViewerFile(virtualPath string) bool { return raster.IsImageViewerFile(virtualPath) }

func decodeImageData(data []byte, format string) (image.Image, string, error) {
	return raster.DecodeImageData(data, format)
}

func decodeImageDataWithMips(data []byte, format string) (image.Image, []image.Image, string, error) {
	return raster.DecodeImageDataWithMips(data, format)
}

func decodeTGA(data []byte) (image.Image, error) { return raster.DecodeTGA(data) }

func decodeRshImage(data []byte, index int) (image.Image, []image.Image, error) {
	return raster.DecodeRshImage(data, index)
}

func RshImages(data []byte) ([]RshImage, error) { return raster.RshImages(data) }

func RshRawImage(data []byte, index int) (RshImage, []RshMip, error) {
	return raster.RshRawImage(data, index)
}

// minInt and maxInt are general helpers, not image code: files across the app
// reach for them, so they stayed behind with the call sites rather than move
// into the decoder package.
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ─── merged from wav_view.go ───────────────────────────────────────────────

func OpenWav(ui *uiState, virtualPath string) {
	if ui.ModTree == nil {
		return
	}
	if idx := FindTabIndex(ui, virtualPath); idx >= 0 {
		ui.TabSel = idx + 1
		EnsureTabClicks(ui)
		ui.StatusText = fmt.Sprintf("Opened %s", filepath.Base(virtualPath))
		return
	}
	data, err := ui.ModTree.ReadFile(virtualPath)
	t := &fileTab{Path: virtualPath, Kind: "wav", WavVolumePct: 100}
	if err != nil {
		t.WavError = err.Error()
	} else {
		t.WavData, err = wav.Parse(data)
		if err != nil {
			t.WavError = err.Error()
		} else if err = StartWavPlayback(ui, t); err != nil {
			t.WavError = err.Error()
		}
	}
	ui.OpenTabs = append(ui.OpenTabs, t)
	ui.TabSel = len(ui.OpenTabs)
	EnsureTabClicks(ui)
	if t.WavError != "" {
		ui.StatusText = "WAV open failed: " + t.WavError
	} else {
		ui.StatusText = "Opened " + filepath.Base(virtualPath)
	}
}

func wavPCM16(data []byte) []int16 {
	samples := make([]int16, len(data)/2)
	for i := range samples {
		samples[i] = int16(binary.LittleEndian.Uint16(data[i*2 : i*2+2]))
	}
	return samples
}

func PauseOtherWavPlayers(ui *uiState, keep *fileTab) {
	for _, other := range ui.OpenTabs {
		if other != keep && other.Kind == "wav" && other.WavPlayer != nil {
			other.WavPlayer.PauseAndStopReading()
			other.WavPaused = true
		}
	}
}

func StartWavPlayback(ui *uiState, t *fileTab) error {
	if t == nil || t.WavData == nil {
		return fmt.Errorf("no WAV data")
	}
	sr, ch := t.WavData.SampleRate, t.WavData.Channels
	ctx, err := audioplayer.Context()
	if err != nil {
		return fmt.Errorf("audio init: %w", err)
	}
	pcm := wavPCM16(t.WavData.PCMData)
	if sr != audioplayer.SampleRate || ch != audioplayer.Channels {
		var err error
		pcm, err = fda.ResamplePCM16(pcm, sr, ch, audioplayer.SampleRate, audioplayer.Channels)
		if err != nil {
			return fmt.Errorf("audio conversion: %w", err)
		}
		sr, ch = audioplayer.SampleRate, audioplayer.Channels
	}
	pcmBytes := fda.PCM16Bytes(pcm)
	cb := audioplayer.NewChunkBuffer(pcmBytes, sr, ch)
	src := audioplayer.NewChunkSource(cb)
	player := ctx.NewPlayer(src)
	player.SetVolume(float64(t.WavVolumePct) / 100)
	FdaPauseAllOthers(ui, nil)
	PauseOtherWavPlayers(ui, t)
	player.Play()
	t.WavPlayer = player
	t.WavSrc = src
	t.WavPaused = false
	t.WavBytesPerSec = int64(sr * ch * 2)
	t.WavTotal = time.Duration(float64(len(pcmBytes)) / float64(t.WavBytesPerSec) * float64(time.Second))
	return nil
}

func WavPosFor(ui *uiState, t *fileTab) time.Duration {
	if t == nil || t.WavPlayer == nil || t.WavSrc == nil || t.WavBytesPerSec == 0 {
		return 0
	}
	heard := t.WavSrc.BytePos() - int64(t.WavPlayer.BufferedSize())
	if heard < 0 {
		heard = 0
	}
	if heard >= int64(t.WavSrc.TotalBytes()) {
		return t.WavTotal
	}
	return time.Duration(float64(heard) / float64(t.WavBytesPerSec) * float64(time.Second))
}

func WavSetVolume(ui *uiState, t *fileTab, pct int) {
	if t == nil {
		return
	}
	if pct < 0 {
		pct = 0
	}
	if pct > 200 {
		pct = 200
	}
	t.WavVolumePct = pct
	if t.WavPlayer != nil {
		t.WavPlayer.SetVolume(float64(pct) / 100)
	}
}

func WavUpdateSliders(ui *uiState, t *fileTab) {
	if t == nil {
		return
	}
	if t.WavTotal > 0 && !t.WavSeekFloat.Dragging() {
		frac := float32(WavPosFor(ui, t)) / float32(t.WavTotal)
		if frac < 0 {
			frac = 0
		}
		if frac > 1 {
			frac = 1
		}
		t.WavSeekFloat.Value = frac
	}
	if t.WavVolumePct <= 0 && t.WavPlayer == nil {
		t.WavVolumePct = 100
	}
	if t.WavVolumePct < 0 {
		t.WavVolumePct = 0
	}
	if t.WavVolumePct > 200 {
		t.WavVolumePct = 200
	}
	if !t.WavVolFloat.Dragging() {
		t.WavVolFloat.Value = float32(t.WavVolumePct) / 200
	}
}

func WavHandleSliderSeek(ui *uiState, t *fileTab) {
	if t == nil || t.WavTotal == 0 || t.WavSrc == nil || t.WavPlayer == nil || !t.WavSeekFloat.Dragging() {
		return
	}
	target := time.Duration(float64(t.WavSeekFloat.Value) * float64(t.WavTotal))
	cur := WavPosFor(ui, t)
	if diff := target - cur; diff > 250*time.Millisecond || diff < -250*time.Millisecond {
		b := int64(float64(target) / float64(time.Second) * float64(t.WavBytesPerSec))
		if t.WavSrc.ChunkBytes() > 0 {
			b -= b % int64(t.WavSrc.ChunkBytes())
		}
		_, _ = t.WavPlayer.Seek(b, io.SeekStart)
	}
}

func WavHandleVolumeSlider(ui *uiState, t *fileTab) {
	if t != nil && t.WavVolFloat.Dragging() {
		WavSetVolume(ui, t, int(t.WavVolFloat.Value*200+0.5))
	}
}

func WavTogglePlayPause(ui *uiState) {
	t := ActiveTab(ui)
	if t == nil || t.Kind != "wav" || t.WavPlayer == nil {
		return
	}
	if t.WavPaused {
		FdaPauseAllOthers(ui, nil)
		PauseOtherWavPlayers(ui, t)
		if t.WavSrc.Remaining() <= 0 {
			if _, err := t.WavPlayer.Seek(0, io.SeekStart); err != nil {
				ui.StatusText = fmt.Sprintf("WAV replay seek failed: %v", err)
				return
			}
		}
		t.WavPlayer.Play()
		t.WavPaused = false
		t.WavSeekFloat.Value = 0
		ui.StatusText = "Playing " + filepath.Base(t.Path)
	} else {
		t.WavPlayer.PauseAndStopReading()
		t.WavPaused = true
		ui.StatusText = "Paused " + filepath.Base(t.Path)
	}
}

func WavStop(ui *uiState) {
	t := ActiveTab(ui)
	if t == nil || t.Kind != "wav" || t.WavPlayer == nil {
		return
	}
	t.WavPlayer.PauseAndStopReading()
	_, _ = t.WavPlayer.Seek(0, io.SeekStart)
	t.WavPaused = true
	t.WavSeekFloat.Value = 0
	ui.StatusText = "Stopped " + filepath.Base(t.Path)
}

func CheckWavAutoRepeat(ui *uiState) {
	t := ActiveTab(ui)
	if t == nil || t.Kind != "wav" || t.WavPlayer == nil || t.WavSrc == nil || t.WavPaused {
		return
	}
	if !t.WavPlayer.IsPlaying() {
		if err := t.WavPlayer.Err(); err != nil {
			t.WavPaused = true
			ui.StatusText = fmt.Sprintf("WAV audio error: %v", err)
		} else if t.WavSrc.Remaining() <= 0 {
			if t.WavRepeat {
				if _, err := t.WavPlayer.Seek(0, io.SeekStart); err == nil {
					t.WavPlayer.Play()
					t.WavPaused = false
					t.WavSeekFloat.Value = 0
					ui.StatusText = "Repeating " + filepath.Base(t.Path)
					return
				}
			}
			t.WavPaused = true
			t.WavSeekFloat.Value = 1
			ui.StatusText = "WAV playback finished"
		} else {
			t.WavPlayer.Play()
		}
	}
}

func wavDuration(t *fileTab) time.Duration {
	if t == nil || t.WavData == nil || t.WavData.SampleRate <= 0 || t.WavData.Channels <= 0 {
		return 0
	}
	return time.Duration(float64(len(t.WavData.PCMData)) / float64(t.WavData.SampleRate*t.WavData.Channels*2) * float64(time.Second))
}

func ConvertWavToFDA(ui *uiState, t *fileTab) {
	if t == nil || t.Kind != "wav" || t.WavData == nil || t.WavError != "" || t.WavExporting {
		return
	}
	t.WavExporting = true
	t.WavExportDone = ""
	fdaData, err := fda.EncodeWAV(t.WavData, 2048)
	if err != nil {
		t.WavExporting = false
		t.WavError = err.Error()
		ui.StatusText = "WAV → FDA failed: " + err.Error()
		return
	}
	target := wavTargetFDA(t.Path)
	diskPath, err := writeFDAOverlay(ui.ModTree, target, fdaData)
	if err != nil {
		t.WavExporting = false
		t.WavError = err.Error()
		ui.StatusText = "WAV → FDA failed: " + err.Error()
		return
	}
	t.WavExporting = false
	t.WavExportDone = diskPath
	ui.StatusText = fmt.Sprintf("Converted %s → %s (%d bytes)", filepath.Base(t.Path), diskPath, len(fdaData))
}

func LayoutWavViewer(ui *uiState, gtx layout.Context, t *fileTab) layout.Dimensions {
	if t == nil {
		return layout.Dimensions{}
	}
	if t.WavError != "" {
		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.Th, unit.Sp(11), "Failed: "+t.WavError)
			l.Color = rgb(0xB00020)
			return l.Layout(gtx)
		})
	}
	if t.WavData == nil {
		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.Th, unit.Sp(11), "No WAV data")
			l.Color = cTextDim
			return l.Layout(gtx)
		})
	}
	WavUpdateSliders(ui, t)
	if t.WavPlayer != nil && !t.WavPaused {
		gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(100 * time.Millisecond)})
	}
	if t.WavConvertClick.Clicked(gtx) {
		ConvertWavToFDA(ui, t)
	}
	pos := WavPosFor(ui, t)
	playLabel := "Play"
	if !t.WavPaused {
		playLabel = "Pause"
	}
	exportLabel := "Convert to FDA"
	if t.WavExporting {
		exportLabel = "Converting…"
	} else if t.WavExportDone != "" {
		exportLabel = "FDA created"
	}
	return layout.Inset{Top: unit.Dp(8), Left: unit.Dp(8), Right: unit.Dp(8), Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						if !t.WavPaused {
							return PrimaryButton(ui, gtx, &t.WavPlayClick, playLabel)
						}
						return SmallButton(ui, gtx, &t.WavPlayClick, playLabel)
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return SmallButton(ui, gtx, &t.WavStopClick, "Stop") }),
					layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						bg := cSaveBg
						if t.WavRepeat {
							bg = cMenuOpen
						}
						return ToggleButton(ui, gtx, &t.WavRepeatClick, "Repeat", t.WavRepeat, bg)
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return SmallButtonDisabled(ui, gtx, &t.WavConvertClick, exportLabel, t.WavExporting)
					}),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{} }),
				)
			}),
			layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						lbl := "Vol"
						if t.WavVolumePct == 0 {
							lbl = "Mute"
						}
						l := material.Label(ui.Th, unit.Sp(9), lbl)
						l.Color = cTextDim
						return l.Layout(gtx)
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						gtx.Constraints.Min.X = gtx.Dp(unit.Dp(70))
						gtx.Constraints.Max.X = gtx.Dp(unit.Dp(110))
						slider := material.Slider(ui.Th, &t.WavVolFloat)
						slider.Color = rgb(0x6B6B6B)
						dims := slider.Layout(gtx)
						WavHandleVolumeSlider(ui, t)
						return dims
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						l := material.Label(ui.Th, unit.Sp(9), fmt.Sprintf("%d%%", t.WavVolumePct))
						l.Color = cTextDim
						return l.Layout(gtx)
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(12)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						l := material.Label(ui.Th, unit.Sp(9), fmtDuration(pos))
						l.Color = cTextDim
						return l.Layout(gtx)
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return LayoutWavWaveform(ui, gtx, t) }),
					layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						l := material.Label(ui.Th, unit.Sp(9), fmtDuration(t.WavTotal))
						l.Color = cTextDim
						return l.Layout(gtx)
					}),
				)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				if t.WavExportDone == "" {
					return layout.Dimensions{}
				}
				l := material.Label(ui.Th, unit.Sp(9), fmt.Sprintf("FDA: %s", filepath.Base(t.WavExportDone)))
				l.Color = rgb(0x2E7D32)
				return layout.Inset{Top: unit.Dp(6)}.Layout(gtx, l.Layout)
			}),
		)
	})
}

func LayoutWavWaveform(ui *uiState, gtx layout.Context, t *fileTab) layout.Dimensions {
	h := gtx.Dp(unit.Dp(28))
	w := gtx.Constraints.Max.X
	if w < gtx.Dp(unit.Dp(80)) {
		w = gtx.Dp(unit.Dp(80))
	}
	sz := image.Pt(w, h)
	gtx.Constraints.Min = sz
	gtx.Constraints.Max = sz
	dims := t.WavSeekFloat.Layout(gtx, layout.Horizontal, unit.Dp(4))
	WavHandleSliderSeek(ui, t)
	if len(t.WavWavePeaks) != w || t.WavWaveW != w {
		t.WavWavePeaks = buildWavePeaks(fda.PCM16Bytes(wavPCM16(t.WavData.PCMData)), w)
		t.WavWaveW = w
	}
	peaks := t.WavWavePeaks
	frac := t.WavSeekFloat.Value
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	playX := clampInt(int(float32(w)*frac), 0, w)

	st := clip.Rect{Max: sz}.Push(gtx.Ops)
	paint.ColorOp{Color: cContentBg}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	st.Pop()
	b := widget.Border{Color: cBorderLight, Width: unit.Dp(1), CornerRadius: unit.Dp(3)}
	gtx.Constraints.Min = sz
	gtx.Constraints.Max = sz
	_ = b.Layout(gtx, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{Size: sz} })

	colPlayed := rgb(0x0078D4)
	colUnplayed := rgb(0x9EA8B3)
	for x := 0; x < w; x++ {
		p := 0
		if x < len(peaks) {
			p = int(peaks[x])
		}
		if p < 2 {
			p = 1
		}
		barH := (h - 6) * p / 255
		if barH < 2 {
			barH = 2
		}
		if barH > h-4 {
			barH = h - 4
		}
		y0 := (h - barH) / 2
		r := image.Rect(x, y0, x+1, y0+barH)
		if r.Min.X < 1 {
			r.Min.X = 1
		}
		if r.Max.X > w-1 {
			continue
		}
		st := clip.Rect(r).Push(gtx.Ops)
		if x < playX {
			paint.ColorOp{Color: colPlayed}.Add(gtx.Ops)
		} else {
			paint.ColorOp{Color: colUnplayed}.Add(gtx.Ops)
		}
		paint.PaintOp{}.Add(gtx.Ops)
		st.Pop()
	}
	if playX >= 1 && playX < w-1 {
		st := clip.Rect(image.Rect(playX, 1, playX+1, h-1)).Push(gtx.Ops)
		paint.ColorOp{Color: rgb(0xE81123)}.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
		st.Pop()
		knobR := gtx.Dp(unit.Dp(3))
		knobRect := image.Rect(playX-knobR, h/2-knobR, playX+knobR, h/2+knobR)
		paint.FillShape(gtx.Ops, rgb(0xE81123), clip.Ellipse(knobRect).Op(gtx.Ops))
	}
	return dims
}

func WavInfoTable(ui *uiState, gtx layout.Context, t *fileTab) layout.Dimensions {
	if t == nil || t.WavData == nil {
		return layout.Dimensions{}
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return TableHead(ui, gtx, "Name", "Value") }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return TableRow2(ui, gtx, "Name", filepath.Base(t.Path), false)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return TableRow2(ui, gtx, "Format", "WAV PCM", true) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return TableRow2(ui, gtx, "Sample rate", fmt.Sprintf("%d Hz", t.WavData.SampleRate), false)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return TableRow2(ui, gtx, "Channels", fmt.Sprintf("%d", t.WavData.Channels), true)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return TableRow2(ui, gtx, "Duration", fmtDuration(wavDuration(t)), false)
		}),
	)
}

// wavTargetFDA is the .fda a .wav converts to, at the same place in the mod.
func wavTargetFDA(virtualPath string) string {
	ext := filepath.Ext(virtualPath)
	return strings.TrimSuffix(virtualPath, ext) + ".fda"
}

// writeFDAOverlay writes converted audio into the mod folder, so ModFolder/Data
// resolution stays in one place.
func writeFDAOverlay(tree *modtree.ModTree, virtualPath string, data []byte) (string, error) {
	if tree == nil {
		return "", fmt.Errorf("no mod loaded")
	}
	return tree.WriteOverlayFile(virtualPath, data)
}
