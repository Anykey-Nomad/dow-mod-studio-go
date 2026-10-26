package app

import (
	"dow-mod-studio/internal/highlight"
	"fmt"
	"log"
	"path/filepath"
	"strings"

	"image/color"

	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

func isTextExtension(ext string) bool {
	switch strings.ToLower(ext) {
	case ".rat", ".con", ".lua", ".ai", ".txt":
		return true
	default:
		return false
	}
}

func TextActive(ui *uiState) bool {
	t := ActiveTab(ui)
	return t != nil && t.Kind == "text" && t.TextError == ""
}

func OpenTextFile(ui *uiState, virtualPath string) {
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
	if err != nil {
		t := &fileTab{Path: virtualPath, Kind: "text", TextError: err.Error()}
		ui.OpenTabs = append(ui.OpenTabs, t)
		ui.TabSel = len(ui.OpenTabs)
		EnsureTabClicks(ui)
		ui.StatusText = fmt.Sprintf("Failed to read %s: %v", filepath.Base(virtualPath), err)
		return
	}
	text := string(data)
	t := &fileTab{
		Path:         virtualPath,
		Kind:         "text",
		TextOriginal: text,
		TextBytes:    len(data),
	}
	t.TextEditor.SingleLine = false
	t.TextEditor.Submit = false
	t.TextEditor.SetText(text)
	ui.OpenTabs = append(ui.OpenTabs, t)
	ui.TabSel = len(ui.OpenTabs)
	EnsureTabClicks(ui)
	ui.StatusText = fmt.Sprintf("Opened %s — %d bytes", filepath.Base(virtualPath), len(data))
	log.Printf("openText %q: %d bytes", virtualPath, len(data))
}

func TextSyncAndApply(ui *uiState, gtx layout.Context) {
	t := ActiveTab(ui)
	if t == nil || t.Kind != "text" || t.TextError != "" {
		return
	}
	for {
		if _, ok := t.TextEditor.Update(gtx); !ok {
			break
		}
	}
	t.TextDirty = t.TextEditor.Text() != t.TextOriginal
}

func HandleTextSave(ui *uiState, t *fileTab) {
	if t == nil || t.Kind != "text" {
		ui.StatusText = "Nothing to save"
		return
	}
	if t.TextLocale {
		// The .ucs tables belong to the game, sit outside the mod tree, and
		// are UTF-16; writing one back as the UTF-8 the editor holds would
		// corrupt it. The tab is read only for the same reason.
		ui.StatusText = fmt.Sprintf("%s is a game locale file — opened read only", filepath.Base(t.Path))
		return
	}
	if t.TextError != "" {
		ui.StatusText = "Save failed: " + t.TextError
		return
	}
	if ui.ModTree == nil {
		ui.StatusText = "Save failed: no mod loaded"
		return
	}
	text := t.TextEditor.Text()
	if text == t.TextOriginal {
		ui.StatusText = fmt.Sprintf("%s — no changes", filepath.Base(t.Path))
		return
	}
	diskPath, err := ui.ModTree.WriteOverlayFile(t.Path, []byte(text))
	if err != nil {
		ui.StatusText = fmt.Sprintf("Save failed: %v", err)
		log.Printf("write text overlay %q: %v", t.Path, err)
		return
	}
	t.TextOriginal = text
	t.TextBytes = len(text)
	t.TextDirty = false
	ui.StatusText = fmt.Sprintf("Saved %s → %s (%d bytes)", filepath.Base(t.Path), diskPath, len(text))
	log.Printf("saved text %q -> %q (%d bytes)", t.Path, diskPath, len(text))
}

func LayoutTextEditor(ui *uiState, gtx layout.Context, t *fileTab) layout.Dimensions {
	// Keep this central panel useful for both short scripts and large Lua files.
	// Gio's multiline editor is constrained by the available center-panel space.
	if t.TextError != "" {
		return layout.Inset{Top: unit.Dp(16), Left: unit.Dp(12), Right: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.Th, unit.Sp(11), "Failed: "+t.TextError)
			l.Color = rgb(0xB00020)
			return l.Layout(gtx)
		})
	}
	editor := func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(6), Left: unit.Dp(8), Right: unit.Dp(8), Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return widgetBorderTextEditor(gtx, ui.Th, &t.TextEditor, t.TextDirty, strings.ToLower(filepath.Ext(t.Path)))
		})
	}
	if !t.TextLocale {
		return editor(gtx)
	}
	// A locale table is a game file being looked at, not a mod file being
	// written, and the read only header says so instead of leaving the tab to
	// look like any other editable one.
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(6), Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				l := material.Label(ui.Th, unit.Sp(10), t.Path+"  —  read only, UTF-16LE .ucs")
				l.Color = cTextDim
				l.MaxLines = 1
				return l.Layout(gtx)
			})
		}),
		layout.Flexed(1, editor),
	)
}

// Kept separate to make the central editor reusable by future script formats.
func widgetBorderTextEditor(gtx layout.Context, th *material.Theme, editor *widget.Editor, dirty bool, ext string) layout.Dimensions {
	borderColor := cBorderLight
	if dirty {
		borderColor = rgb(0x0078D4)
	}
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	gtx.Constraints.Min.Y = gtx.Constraints.Max.Y
	return widget.Border{Color: borderColor, Width: unit.Dp(1), CornerRadius: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8), Top: unit.Dp(6), Bottom: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			ed := material.Editor(th, editor, "")
			ed.TextSize = unit.Sp(12)
			ed.HintColor = cTextDim
			// Text is painted by the syntax layer below. Gio still owns the
			// caret, selection, keyboard input and scrolling operations.
			ed.Color = color.NRGBA{}
			return layout.Stack{}.Layout(gtx,
				layout.Expanded(func(hgtx layout.Context) layout.Dimensions {
					paintSyntaxHighlight(hgtx, th, editor, editor.Text(), ext)
					return layout.Dimensions{Size: hgtx.Constraints.Max}
				}),
				layout.Stacked(func(egt layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(syntaxGutterDp)}.Layout(egt, ed.Layout)
				}),
			)
		})
	})
}

// ── syntax highlighting (internal/highlight) ─────────────────────────────────
//
// The tokenizer and the painter live in internal/highlight; the editors call
// them through these names, and the dim gutter colour stays this program's.

const syntaxGutterDp = highlight.SyntaxGutterDp

func paintSyntaxHighlight(gtx layout.Context, th *material.Theme, editor *widget.Editor, source, ext string) {
	highlight.PaintSyntaxHighlight(gtx, th, editor, source, ext, cTextDim)
}

func itoa(v int) string { return highlight.Itoa(v) }
