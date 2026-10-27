package shell

import (
	"dow-mod-studio/internal/app/editors"
	"dow-mod-studio/internal/app/fileops"
	"dow-mod-studio/internal/app/render"
	"dow-mod-studio/internal/app/uikit"
	"dow-mod-studio/internal/app/uistate"
	appunit "dow-mod-studio/internal/app/unit"
	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/rgd"
	"fmt"
	"gioui.org/app"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"image"
	"image/color"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Rgb lives in uikit; the alias keeps every call site as it was.
var Rgb = uikit.Rgb

func Rgba(r, g, b, a uint8) color.NRGBA { return color.NRGBA{R: r, G: g, B: b, A: a} }

// SetColorTheme lives in uikit with the palette it paints.
var SetColorTheme = uikit.SetColorTheme

func ApplyMaterialTheme(th *material.Theme, dark bool) {
	SetColorTheme(dark)
	if th == nil {
		return
	}
	th.Palette = material.Palette{
		Bg: uikit.CBg, Fg: uikit.CText, ContrastBg: uikit.CSaveBg, ContrastFg: uikit.CText,
	}
}

type MenuDef struct {
	Name  string
	Items []string
}

var Menus []MenuDef

func Init() {
	if runtime.GOOS == "windows" {
		Menus = []MenuDef{
			{Name: "File", Items: []string{"New Mod...", "Open Module...", "Close", "—", "Settings", "—", "Associate .module files", "Remove .module association", "—", "Recent Modules", "View Recycle Bin", "Empty Recycle Bin", "Exit"}},
			{Name: "Mod", Items: []string{"Save", "Export File", "—", "Pack SGA...", "Extract SGA...", "—", "Burn Mod", "Module Settings..."}},
			{Name: "Help", Items: []string{"About DoW Mod Studio", "Documentation", "—", "Open log folder", "Check for Updates"}},
		}
	} else {
		Menus = []MenuDef{
			{Name: "File", Items: []string{"New Mod...", "Open Module...", "Close", "—", "Settings", "—", "Recent Modules", "View Recycle Bin", "Empty Recycle Bin", "Exit"}},
			{Name: "Mod", Items: []string{"Save", "Export File", "—", "Pack SGA...", "Extract SGA...", "—", "Burn Mod", "Module Settings..."}},
			{Name: "Help", Items: []string{"About DoW Mod Studio", "Documentation", "—", "Open log folder", "Check for Updates"}},
		}
	}
}

// UiState is the whole UI state; the type lives in uistate so the
// feature packages can share it.
type UiState = uistate.State

type RgdEditorClickState = uistate.RgdEditorClickState

// FileTab holds per-opened-file state (RGD or FDA) — one tab per file in the mod tree.
type FileTab = uistate.Tab

type RgdVisible = uistate.RgdVisible

// pointerTag identifies a pointer target of the app's own making, as opposed
// to a widget's, which brings its own tag with it.
//
// A tag has to be a distinct pointer, and a zero sized one is not:
// new(struct{}) hands out runtime.zerobase every time, so every zero sized tag
// in a program compares equal. Two targets sharing a tag share one queue of
// events, and whichever handler asks first takes them: the tab bar's handler
// runs before the file tree's, so it took the tree's press, the tree's press
// never opened anything, and a right press in the tree opened the tab bar's
// menu instead of the file's. The name is only here to make the tags readable
// in a debugger.
// NewPointerTag lives in uikit; the alias keeps the call sites.
var NewPointerTag = uikit.NewPointerTag

func NewUI(th *material.Theme) *UiState {
	s := &UiState{Th: th, MenuOpen: -1, TabSel: 0, DarkTheme: true, GpuAPI: render.DefaultGPUAPI, KeyboardArea: "files", ImageCache: make(map[string]editors.ImageCacheEntry), ModRefreshCh: make(chan fileops.ModCatalogRefreshResult, 1), ModFileWatchSignal: make(chan struct{}, 1), StateDirty: true, LeftTreePointerTag: NewPointerTag("left-tree"), TabBarPointerTag: NewPointerTag("tab-bar"), Locale: editors.LocaleDefault}
	ApplyMaterialTheme(th, true)
	s.MenuClicks = make([]widget.Clickable, len(Menus))
	s.MenuItemClicks = make([][]widget.Clickable, len(Menus))
	for i := range Menus {
		s.MenuItemClicks[i] = make([]widget.Clickable, len(Menus[i].Items))
	}
	s.Expanded = map[string]bool{"Data": true}
	s.NewObjNameEditor.SingleLine = true
	s.NewObjNameEditor.Submit = true
	s.SettingsGameEditor.SingleLine = true
	s.SettingsGameEditor.Submit = true
	s.SettingsFormatEditor.SingleLine = true
	s.SettingsFormatEditor.Submit = true
	// The frames field beside the shift buttons in the bone pose bar: a
	// whole number, with 1 standing in when it is empty or unreadable.
	s.UnitBoneKeyShiftEd.SingleLine = true
	s.UnitBoneKeyShiftEd.Submit = true
	s.UnitBoneKeyShiftEd.SetText("1")
	// The formats in force before any state file is read, so a frame laid
	// out before the settings are applied already opens the built-in list.
	s.TextEditorFormats = append([]string(nil), fileops.DefaultTextFormats...)
	s.SearchEditor.SingleLine = true
	s.SearchEditor.Submit = true
	s.SearchKind = modtree.SearchAll
	s.SearchKindClicks = make([]widget.Clickable, len(modtree.SearchKinds))
	appunit.EnsureUnitClicks(s)
	s.TabClicks = make([]widget.Clickable, 1) // Module only
	s.TabCloses = nil
	s.OpenTabs = nil
	return s
}

// EnsureTabClicks lives in uikit; the alias keeps every call site as it was.
var EnsureTabClicks = uikit.EnsureTabClicks

// FindTabIndex lives in uikit; the alias keeps every call site as it was.
var FindTabIndex = uikit.FindTabIndex

func ActiveTab(ui *UiState) *FileTab {
	if ui.TabSel <= 0 || ui.TabSel > len(ui.OpenTabs) {
		return nil
	}
	return ui.OpenTabs[ui.TabSel-1]
}

func IsModuleActive(ui *UiState) bool { return ui.TabSel == 0 }

func FdaActive(ui *UiState) bool {
	t := ActiveTab(ui)
	if t == nil {
		return false
	}
	return t.Kind == "fda" && (t.FdaFile != nil || t.FdaError != "")
}

func RgdActive(ui *UiState) bool {
	t := ActiveTab(ui)
	if t == nil {
		return false
	}
	return t.Kind == "rgd" && (t.RgdFile != nil || t.RgdError != "")
}

// releaseTabResources hands back everything a tab holds outside the tab list:
// a running decode, the 3D preview's buffers, any audio player, and the
// dropdowns that belong to the tab. Closing one tab and closing them all both
// go through it, so a tab can never be dropped with something of it still
// running — which is what used to happen when they were closed in bulk.
func ReleaseTabResources(ui *UiState, t *FileTab) {
	if t == nil {
		return
	}
	// A dropdown belongs to the tab that is going away; leaving it open would
	// draw it over whatever takes its place.
	if appunit.IsUnitTab(t) {
		ui.UnitRaceOpen = false
		ui.UnitListOpen = false
	}
	if t.ImageDecodeStop != nil {
		t.ImageDecodeStop()
		t.ImageDecodeStop = nil
	}
	// The 3D preview renders on a worker; drop the reference so a pending
	// channel write cannot keep the model alive after the tab is gone.
	if t.Model != nil {
		// The hardware renderer caches the model's buffers, which go back to
		// the driver along with the tab. Only an existing handle is asked, so
		// closing a tab never brings one up.
		if ui.ModelGpu != nil && t.Model.Model != nil {
			ui.ModelGpu.Forget(&t.Model.Model.Model)
		}
		t.Model = nil
	}
	if t.Kind == "fda" && t.FdaPlayer != nil {
		t.FdaPlayer.PauseAndStopReading()
		t.FdaPlayer = nil
	}
	if t.Kind == "wav" && t.WavPlayer != nil {
		t.WavPlayer.PauseAndStopReading()
		t.WavPlayer = nil
	}
}

func CloseTabAt(ui *UiState, idx int) {
	if idx < 0 || idx >= len(ui.OpenTabs) {
		return
	}
	t := ui.OpenTabs[idx]
	ReleaseTabResources(ui, t)
	oldSel := ui.TabSel
	ui.OpenTabs = append(ui.OpenTabs[:idx], ui.OpenTabs[idx+1:]...)
	// splice clickables to keep per-tab button state aligned
	// tabClicks: [0]=Module, [1]=tab0, [2]=tab1, ...
	if idx+1 < len(ui.TabClicks) {
		ui.TabClicks = append(ui.TabClicks[:idx+1], ui.TabClicks[idx+2:]...)
	}
	if idx < len(ui.TabCloses) {
		ui.TabCloses = append(ui.TabCloses[:idx], ui.TabCloses[idx+1:]...)
	}
	newLen := len(ui.OpenTabs)
	if oldSel == idx+1 {
		ui.TabSel = oldSel
		if ui.TabSel > newLen {
			ui.TabSel = newLen
		}
	} else if oldSel > idx+1 {
		ui.TabSel = oldSel - 1
		if ui.TabSel > newLen {
			ui.TabSel = newLen
		}
	} else {
		if ui.TabSel > newLen {
			ui.TabSel = newLen
		}
	}
	EnsureTabClicks(ui)
}

func CloseActiveFileTab(ui *UiState) {
	if ui.TabSel == 0 {
		return
	}
	CloseTabAt(ui, ui.TabSel-1)
}

func CloseAllFileTabs(ui *UiState) {
	for _, t := range ui.OpenTabs {
		ReleaseTabResources(ui, t)
	}
	// The object window's viewport is not a file tab, but it holds the same
	// viewer, and the mod it reads from is about to be closed underneath it.
	appunit.ResetUnitModel(ui)
	ui.OpenTabs = nil
	ui.TabSel = 0
	// The bar is empty now, so any scroll offset into it is meaningless and
	// has to go back to the left.
	ui.TabScrollX = 0
	CloseTabContextMenu(ui)
	EnsureTabClicks(ui)
}

func ClearRgd(ui *UiState) {
	t := ActiveTab(ui)
	if t != nil && t.Kind == "rgd" {
		CloseTabAt(ui, ui.TabSel-1)
	}
}

func ClearFda(ui *UiState) {
	t := ActiveTab(ui)
	if t != nil && t.Kind == "fda" {
		CloseTabAt(ui, ui.TabSel-1)
		return
	}
}

func CloseActiveViewer(ui *UiState) { CloseActiveFileTab(ui) }

// FindFirstModuleArg returns the first CLI arg that looks like a .module file (ignoring flags).
// It strips surrounding quotes if present.
func FindFirstModuleArg() string {
	for _, a := range os.Args[1:] {
		if strings.HasPrefix(a, "-") {
			continue
		}
		trim := strings.Trim(a, "\"'")
		trim = strings.TrimSpace(trim)
		if trim == "" {
			continue
		}
		// Normalize slashes
		if strings.EqualFold(filepath.Ext(trim), ".module") {
			return trim
		}
		// Also accept plain name without extension that resolves to .module via fallback?
		// Don't return non-.module paths.
	}
	return ""
}

func SyncWindowTitle(ui *UiState) {
	if ui.Win == nil {
		return
	}
	if ui.ModulePath != "" {
		ui.Win.Option(app.Title(fmt.Sprintf("%s — DoW Mod Studio", filepath.Base(ui.ModulePath))))
	} else {
		ui.Win.Option(app.Title("DoW Mod Studio — No module"))
	}
}

func TryAutoLoad(ui *UiState) {
	// 1) Highest priority: explicit .module file from OS (double-click / Open With / CLI)
	if arg := FindFirstModuleArg(); arg != "" {
		// Handle quoted, relative, and bare name forms similar to dialog resolution
		candidate := arg
		// If relative or bare, try to resolve
		if !filepath.IsAbs(candidate) {
			if abs, err := filepath.Abs(candidate); err == nil {
				if _, err2 := os.Stat(abs); err2 == nil {
					candidate = abs
				} else {
					// Try default DoW DE dir
					try := filepath.Join(`C:/Program Files/Steam/steamapps/common/Dawn of War Definitive Edition`, candidate)
					if _, err3 := os.Stat(try); err3 == nil {
						candidate = try
					} else if !strings.EqualFold(filepath.Ext(candidate), ".module") {
						try2 := candidate + ".module"
						try2Abs := filepath.Join(`C:/Program Files/Steam/steamapps/common/Dawn of War Definitive Edition`, try2)
						if _, err4 := os.Stat(try2Abs); err4 == nil {
							candidate = try2Abs
						} else if abs2, err5 := filepath.Abs(try2); err5 == nil {
							if _, err6 := os.Stat(abs2); err6 == nil {
								candidate = abs2
							}
						}
					}
				}
			}
		}
		// Bare name without .module + path fallback also tried above
		if !strings.EqualFold(filepath.Ext(candidate), ".module") && strings.TrimSpace(candidate) != "" {
			if _, err := os.Stat(candidate + ".module"); err == nil {
				candidate = candidate + ".module"
			}
		}
		if _, err := os.Stat(candidate); err == nil {
			LoadModule(ui, candidate)
			return
		}
		// If arg looked like .module but file not found, show error and don't fall back silently
		if strings.EqualFold(filepath.Ext(arg), ".module") {
			ui.StatusText = fmt.Sprintf("Module not found: %s — File → Open Module…", arg)
			log.Printf("tryAutoLoad: arg %q resolved to %q not found", arg, candidate)
			SyncWindowTitle(ui)
			return
		}
	}

	candidates := []string{}
	if ui.GamePath != "" {
		candidates = append(candidates, filepath.Join(ui.GamePath, "DXP3.module"), filepath.Join(ui.GamePath, "W40k.module"))
	}
	candidates = append(candidates,
		`C:/Program Files/Steam/steamapps/common/Dawn of War Definitive Edition/DXP3.module`,
		`C:/Program Files/Steam/steamapps/common/Dawn of War Definitive Edition/W40k.module`,
		`C:/Program Files/Steam/steamapps/common/Dawn of War Definitive Edition/DXP2.module`,
		`C:/Program Files/Steam/steamapps/common/Dawn of War Definitive Edition/WXP.module`,
		`C:/Program Files/Steam/steamapps/common/Dawn of War Definitive Edition/DoWDE.module`,
		"DXP3.module",
		"W40k.module",
	)
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			LoadModule(ui, p)
			return
		}
	}
	ui.StatusText = "No module loaded — File → Open Module…"
	SyncWindowTitle(ui)
}

func LoadModule(ui *UiState, path string) {
	// stop audio and close all file tabs before tearing down tree
	fileops.StopModFileWatcher(ui)
	CloseAllFileTabs(ui)
	editors.ClearImageCache(ui)
	if ui.ModTree != nil {
		ui.ModTree.Close()
		ui.ModTree = nil
	}
	// The previous mod's strings must stop being searched for before the new
	// mod's folder is known, or a table built now would still carry them.
	ui.ProjectDir = ""
	editors.SetLocaleOverrideDir("")
	abs, err := filepath.Abs(path)
	if err == nil {
		path = abs
	}
	// clear previous state
	ui.Expanded = map[string]bool{"Data": true}
	ui.SelectedPath = ""
	ui.SelectedTreePath = "Data"
	ui.KeyboardArea = "files"
	ui.VisibleNodes = nil
	ui.RowClicks = nil

	// The hashed field names inside a .rgd resolve through the game's own
	// dictionary, which ships in the game folder rather than beside the
	// executable; the hard coded Windows install path in InitDict is only a
	// last resort. The module sits in the game folder, so its directory says
	// where to look before anything parses a .rgd.
	rgd.AddDictRoot(filepath.Dir(path))
	tree, err := modtree.BuildModTree(path, "English")
	if err != nil {
		ui.StatusText = fmt.Sprintf("Failed to load %s: %v", filepath.Base(path), err)
		log.Printf("loadModule %q: %v", path, err)
		if tree != nil {
			tree.Close()
		}
		ui.ModulePath = ""
		SyncWindowTitle(ui)
		return
	}
	ui.ModTree = tree
	ui.ModulePath = path
	if tree.AppPath != "" {
		ui.GamePath = tree.AppPath
	}
	RememberRecentFolder(ui, filepath.Dir(path))
	RememberRecentModule(ui, path)
	ui.ModSnapshot = fileops.CurrentModSnapshot(tree)
	fileops.StartModFileWatcher(ui, tree)
	ui.LastModPoll = time.Now()
	// The mod's own project folder has to exist and be known before the string
	// table is built, because it is searched first: a localisation written into
	// it has to be in the table the same moment it is written. A failure here
	// must not stop the module from opening, so it is logged and the message
	// folded into the status line instead.
	if dir, perr := fileops.EnsureProjectDirs(tree, path); perr != nil {
		log.Printf("projects folder for %s: %v", filepath.Base(path), perr)
		ui.ProjectStatusErr = perr
	} else {
		ui.ProjectDir = dir
		ui.UnitsDir = filepath.Join(dir, fileops.UnitsDirName)
		ui.ProjectStatusErr = nil
	}
	editors.SetLocaleOverrideDir(ui.ProjectDir)
	// A module brings its own Locale folder, and can add languages the engine
	// does not ship, so the picker is rediscovered with the module in place.
	// It also rebuilds the string table for the language already chosen.
	editors.RefreshLocales(ui)
	// clear viewers (already closed)
	CloseAllFileTabs(ui)
	// expand Data only by default (user can drill down)
	ui.Expanded = map[string]bool{"Data": true}
	f, fi := tree.Count()
	ui.StatusText = fmt.Sprintf("Loaded %s — %d folders, %d files (%d sources)", filepath.Base(path), f, fi, len(tree.Sources))
	if len(tree.Warnings) > 0 {
		log.Printf("warnings loading %s:", path)
		for _, w := range tree.Warnings {
			log.Printf("  %s", w)
		}
	}
	// if tree is tiny, auto-expand one more level
	if f < 50 {
		// expand children of Data
		for _, c := range tree.Root.Children {
			ui.Expanded[c.FullPath] = true
		}
	}
	// The result list points at files of the previous module, so drop it.
	ui.SearchHits = nil
	ui.SearchSel = 0
	RunSearch(ui, true)
	if ui.ProjectStatusErr != nil {
		ui.StatusText += fmt.Sprintf(" — projects folder unavailable: %v", ui.ProjectStatusErr)
	}
	SyncWindowTitle(ui)
	MarkStateDirty(ui)
}

// HLine lives in uikit; the alias keeps every call site as it was.
var HLine = uikit.HLine

// FillRect lives in uikit; the alias keeps every call site as it was.
var FillRect = uikit.FillRect

// appunit.Rrect lives in uikit; the alias keeps every call site as it was.
var Rrect = uikit.Rrect

func LayoutWaveform(ui *UiState, gtx layout.Context, t *FileTab) layout.Dimensions {
	h := gtx.Dp(unit.Dp(28))
	// When called from Flexed, Max/Min are already the flexed width.
	w := gtx.Constraints.Max.X
	if w < gtx.Dp(unit.Dp(80)) {
		w = gtx.Dp(unit.Dp(80))
	}
	sz := image.Pt(w, h)
	// Constrain to exact size
	gtx.Constraints.Min = sz
	gtx.Constraints.Max = sz

	// Input: use existing seek Float so position stays in sync with audio.
	// Float.Layout registers pointer handling and updates Value on drag/click.
	dims := t.FdaSeekFloat.Layout(gtx, layout.Horizontal, unit.Dp(4))
	// Apply seek if user dragged/clicked
	editors.FdaHandleSliderSeek(ui, gtx, t)

	// Build peaks if width changed or empty
	if len(t.FdaWavePeaks) != w || t.FdaWaveW != w {
		t.FdaWavePeaks = editors.BuildWavePeaks(t.FdaPcmBytes, w)
		t.FdaWaveW = w
	}
	peaks := t.FdaWavePeaks
	frac := t.FdaSeekFloat.Value
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	playX := int(float32(w) * frac)
	if playX < 0 {
		playX = 0
	}
	if playX > w {
		playX = w
	}

	// Background
	bg := uikit.CContentBg
	stack := clip.Rect{Max: sz}.Push(gtx.Ops)
	paint.ColorOp{Color: bg}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	stack.Pop()

	// Border
	b := widget.Border{Color: uikit.CBorderLight, Width: unit.Dp(1), CornerRadius: unit.Dp(3)}
	gtx.Constraints.Min = sz
	gtx.Constraints.Max = sz
	_ = b.Layout(gtx, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{Size: sz} })

	// Centre line (subtle)
	midY := h/2 - 1
	st := clip.Rect(image.Rect(1, midY, w-1, midY+1)).Push(gtx.Ops)
	paint.ColorOp{Color: Rgba(0, 0, 0, 10)}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	st.Pop()

	// Bars — 1px wide, gap 1px when w is large enough? For now 1px per column, topped/bottom centered.
	// Draw in two passes: unplayed grey, played blue, for speed use single loop.
	colPlayed := Rgb(0x0078D4)
	colUnplayed := Rgb(0x9EA8B3)
	// Slight dim for very low peaks: still visible
	for x := 0; x < w; x++ {
		p := 0
		if peaks != nil && x < len(peaks) {
			p = int(peaks[x])
		}
		if p == 0 {
			p = 1 // ensure 1px line for silence so track not empty
		}
		barH := (h - 6) * p / 255
		if barH < 2 {
			barH = 2
		}
		if barH > h-4 {
			barH = h - 4
		}
		y0 := (h - barH) / 2
		y1 := y0 + barH
		// clip 1px wide bar (leave 0 gap for dense view; if w>300 we could add gap but keep 1px)
		r := image.Rect(x, y0, x+1, y1)
		if r.Max.X > w {
			continue
		}
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
	// Playhead line 1px
	if playX >= 1 && playX < w-1 {
		st := clip.Rect(image.Rect(playX, 1, playX+1, h-1)).Push(gtx.Ops)
		paint.ColorOp{Color: Rgb(0xE81123)}.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
		st.Pop()
		knobR := gtx.Dp(unit.Dp(3))
		knobRect := image.Rect(playX-knobR, h/2-knobR, playX+knobR, h/2+knobR)
		paint.FillShape(gtx.Ops, Rgb(0xE81123), clip.Ellipse(knobRect).Op(gtx.Ops))
	}

	return dims
}
