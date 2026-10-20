package main

import (
	"dow-mod-studio/internal/whe"
	"fmt"
	"image"
	"sort"
	"strings"
	"time"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

// ── the Classic OE section ─────────────────────────────────────────────────
//
// The classic object editor's shape, as a section of the object tab: a column
// of collapsible header panels down the left — Model Properties, Selected UI,
// the condition / event / modifier lists, and the anims, motions and actions
// tree — with the unit's model taking everything else. The middle of that
// editor was a grey render area, and here it is the same preview the Viewport
// section carries, so picking a clip in the left panel plays it in the middle
// rather than doing nothing.
//
// The panels are headers first, but the three list panels — conditions,
// events and modifiers — read the unit's .whe, which the Events section
// already parses into a document; the same loader fills them here, so the two
// sections never disagree about what the file says. The two property panels
// at the top still expand onto their hint text, which says what will live
// there.

// unitSectionClassicOE names the section, so the layout does not repeat the
// string and a rename cannot miss a branch.
const unitSectionClassicOE = "Classic OE"

// classicOEPanelDp is the width of the collapsible panel column.
const classicOEPanelDp = 200

// The panel kinds. A panel's kind says what its expanded body draws; the
// empty kind falls back to the panel's hint text.
const (
	classicOEKindHint       = ""
	classicOEKindAnims      = "anims"
	classicOEKindEvents     = "events"
	classicOEKindConditions = "conditions"
	classicOEKindModifiers  = "modifiers"
)

// classicOEHeaders are the panels of the classic editor's left column, in the
// order that editor showed them. The hint is what an expanded panel with
// nothing in it yet says instead of showing an empty hole.
var classicOEHeaders = []struct {
	label string
	kind  string
	hint  string
}{
	{"Model Properties", classicOEKindHint, "The model's cameras, bones and materials will be listed here."},
	{"Selected UI", classicOEKindHint, "The selected entry's UI properties will be listed here."},
	{"Condition List", classicOEKindConditions, ""},
	{"Event List", classicOEKindEvents, ""},
	{"Modifier List", classicOEKindModifiers, ""},
	{"Anims / Motions / Actions", classicOEKindAnims, ""},
}

// classicOEAnimsHeader is the index of the one panel that has content: its
// expanded body is the model's clip list. It is derived rather than counted
// by hand, so reordering the panel list cannot silently point the body at the
// wrong panel.
var classicOEAnimsHeader = len(classicOEHeaders) - 1

// ensureClassicOEClicks sizes the panels' click targets and open flags to the
// panel list. The anims panel starts expanded, since it is the reason the
// column is there.
func (ui *uiState) ensureClassicOEClicks() {
	if len(ui.classicOEClicks) != len(classicOEHeaders) {
		ui.classicOEClicks = make([]widget.Clickable, len(classicOEHeaders))
	}
	if len(ui.classicOEOpen) != len(classicOEHeaders) {
		ui.classicOEOpen = make([]bool, len(classicOEHeaders))
		ui.classicOEOpen[classicOEAnimsHeader] = true
	}
}

// layoutClassicOEArea is the whole section: the panel column, then the
// preview taking the rest. The model load is kicked off here the same way the
// Viewport section kicks it off, so opening the section on a unit that has not
// been previewed yet still ends with a model on screen.
func (ui *uiState) layoutClassicOEArea(gtx layout.Context) layout.Dimensions {
	ui.ensureUnitModel()
	// The .whe behind the condition, event and modifier panels: the Events
	// section's loader, which reads the file once per unit and remembers why
	// there is nothing to read. Calling it here means the panels are filled on
	// a first visit to Classic OE rather than only after the Events section
	// has been opened once.
	ui.ensureUnitEvents()
	ui.ensureClassicOEClicks()
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			panelW := gtx.Dp(unit.Dp(classicOEPanelDp))
			gap := gtx.Dp(unit.Dp(unitColGapDp))
			return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
				layout.Rigid(ui.fixedPanel(panelW, ui.layoutClassicOELeftPanel)),
				layout.Rigid(layout.Spacer{Width: unit.Dp(gap)}.Layout),
				layout.Flexed(1, ui.layoutClassicOEPreview),
			)
		}),
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			if ui.classicOEDialog == classicOEDialogNone {
				return layout.Dimensions{}
			}
			return ui.layoutClassicOEEventDialog(gtx)
		}),
	)
}

// layoutClassicOELeftPanel is the column of collapsible headers, framed and
// scrollable: the anims tree alone is taller than a small window, and the
// panels below it must stay reachable when it is.
func (ui *uiState) layoutClassicOELeftPanel(gtx layout.Context) layout.Dimensions {
	return panelFrame(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.layoutUnitColumnHeader(gtx, "Object Editor")
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, cHeaderLine, 1) }),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return ui.panelWithScroll(gtx, &ui.classicOEScroll, ui.layoutClassicOEHeaderList)
			}),
		)
	})
}

// layoutClassicOEHeaderList stacks every panel: its header row, and the body
// underneath when the panel is open.
func (ui *uiState) layoutClassicOEHeaderList(gtx layout.Context) layout.Dimensions {
	ui.ensureClassicOEClicks()
	var children []layout.FlexChild
	for i := range classicOEHeaders {
		i := i
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.layoutClassicOEHeaderRow(gtx, i)
		}))
		if ui.classicOEOpen[i] {
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.layoutClassicOEHeaderBody(gtx, i)
			}))
		}
	}
	return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	})
}

// layoutClassicOEHeaderRow is one panel header: a bordered bar with the label
// centred, the way the classic editor drew its collapse bars. The whole bar is
// the click target, open or closed.
func (ui *uiState) layoutClassicOEHeaderRow(gtx layout.Context, index int) layout.Dimensions {
	ui.ensureClassicOEClicks()
	if index < 0 || index >= len(classicOEHeaders) {
		return layout.Dimensions{}
	}
	c := &ui.classicOEClicks[index]
	if c.Clicked(gtx) {
		ui.classicOEOpen[index] = !ui.classicOEOpen[index]
	}
	open := ui.classicOEOpen[index]
	bg := cPanelBg
	if open {
		bg = cHeaderBg
	} else if c.Hovered() {
		bg = cMenuHover
	}
	return layout.Inset{Top: unit.Dp(2), Bottom: unit.Dp(2), Left: unit.Dp(4), Right: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return c.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			h := gtx.Dp(unit.Dp(22))
			// The bar spans the column's width, so the height is
			// pinned before the stack paints: with no minimum the
			// background would fall back to the column's full height.
			gtx.Constraints.Min = image.Pt(gtx.Constraints.Max.X, h)
			gtx.Constraints.Max = image.Pt(gtx.Constraints.Max.X, h)
			return layout.Stack{}.Layout(gtx,
				layout.Expanded(func(gtx layout.Context) layout.Dimensions {
					rrect(gtx, bg, gtx.Constraints.Max, gtx.Dp(2))
					widget.Border{Color: cBorder, Width: unit.Dp(1), CornerRadius: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layout.Dimensions{Size: gtx.Constraints.Max}
					})
					return layout.Dimensions{Size: gtx.Constraints.Max}
				}),
				layout.Stacked(func(gtx layout.Context) layout.Dimensions {
					return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						l := material.Label(ui.th, unit.Sp(10), classicOEHeaders[index].label)
						l.Color = cText
						l.MaxLines = 1
						return l.Layout(gtx)
					})
				}),
			)
		})
	})
}

// layoutClassicOEHeaderBody is what an open panel shows: the list its kind
// names, or the panel's hint for the ones whose content is not built yet.
func (ui *uiState) layoutClassicOEHeaderBody(gtx layout.Context, index int) layout.Dimensions {
	switch classicOEHeaders[index].kind {
	case classicOEKindAnims:
		return ui.layoutClassicOEAnimTree(gtx)
	case classicOEKindEvents:
		return ui.layoutClassicOEEventList(gtx)
	case classicOEKindConditions:
		return ui.layoutClassicOEConditionList(gtx)
	case classicOEKindModifiers:
		return ui.layoutClassicOEModifierList(gtx)
	}
	return classicOEHint(gtx, ui.th, classicOEHeaders[index].hint)
}

// classicOEHint is the dim one-paragraph body of a panel with no content yet.
func classicOEHint(gtx layout.Context, th *material.Theme, msg string) layout.Dimensions {
	return layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		l := material.Label(th, unit.Sp(9), msg)
		l.Color = cTextDim
		return l.Layout(gtx)
	})
}

// classicOEListDoc is the .whe document behind the three list panels, and
// what to say when there is none. The second result is empty when the first
// is a document.
func (ui *uiState) classicOEListDoc() (*whe.Document, string) {
	if ui.unitEvtDoc == nil {
		msg := ui.unitEvtErr
		if msg == "" {
			msg = "This unit has no .whe file to read."
		}
		return nil, msg
	}
	return ui.unitEvtDoc, ""
}

// classicOEStaticRow is one line of a list panel: the name in the ordinary
// colour and a dim note beside it, without the hover treatment of a list whose
// rows are pressed — nothing here is clickable yet, and a row that lights up
// under the pointer would promise otherwise.
func (ui *uiState) classicOEStaticRow(gtx layout.Context, name, note string) layout.Dimensions {
	return layout.Inset{Top: unit.Dp(1), Bottom: unit.Dp(1)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				l := material.Label(ui.th, unit.Sp(10), unitViewportRowText(name))
				l.Color = cText
				l.MaxLines = 1
				return l.Layout(gtx)
			}),
			layout.Flexed(1, rowGap()),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				if note == "" {
					return layout.Dimensions{}
				}
				l := material.Label(ui.th, unit.Sp(9), note)
				l.Color = cTextDim
				l.MaxLines = 1
				return l.Layout(gtx)
			}),
		)
	})
}

// classicOERows stacks list rows in the panel's side inset, so the three list
// panels line up under their header bars.
func classicOERows(gtx layout.Context, rows []layout.FlexChild) layout.Dimensions {
	return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, rows...)
	})
}

// classicOENote keeps a note — an effect path above all — beside its name
// rather than pushing the name out of the panel: the .whe's fx paths run to
// fifty characters and the column is two hundred pixels wide.
func classicOENote(note string) string {
	if len(note) > 26 {
		return note[:23] + "…"
	}
	return note
}

// classicOEUseNote is the note on the condition and modifier rows: how many
// choices or motions carry it, phrased for one or many.
func classicOEUseNote(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

// layoutClassicOEAnimList is the anims panel's body: the model's clips, the
// one being played marked, each press playing it in the preview beside the
// panel. It is the Viewport section's own list with its own click targets, so
// the two views cannot fight over which clip a press meant.
func (ui *uiState) layoutClassicOEAnimList(gtx layout.Context) layout.Dimensions {
	v := ui.unitModelViewer()
	if v == nil || v.model == nil {
		l := material.Label(ui.th, unit.Sp(9), "Loading the model…")
		l.Color = cTextDim
		return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, l.Layout)
	}
	return ui.unitAnimListWithClicks(gtx, v, &ui.classicOEAnimClicks)
}

const (
	// The four folders of the classic editor's tree, in the order the
	// editor listed them: the clip names first, then the .whe's own
	// motion (timeline) and action records, and finally the action
	// hierarchy built from action-to-subaction links.
	classicOEFolderAnims = iota
	classicOEFolderMotions
	classicOEFolderActions
	classicOEFolderTree
	classicOEFolderCount
)

// classicOETreeMaxDepth caps the nesting of the derived tree so that a .whe
// with a self-referential link cannot recurse without end. Deeper children
// stop at this depth; the row itself still renders.
const classicOETreeMaxDepth = 6

// ensureClassicOEFolders starts the folders with Animations and Action Tree
// open, as the editor opened them, and leaves the other two closed because
// the counts alone already answer what they hold. A missing expansion record
// means "the default", so the maps only need to exist.
func (ui *uiState) ensureClassicOEFolders() {
	if !ui.classicOEFolderInit {
		ui.classicOEFolderInit = true
		ui.classicOEFolderOpen = [classicOEFolderCount]bool{}
		ui.classicOEFolderOpen[classicOEFolderAnims] = true
		ui.classicOEFolderOpen[classicOEFolderTree] = true
	}
	if ui.classicOEExpandClicks == nil {
		ui.classicOEExpandClicks = make(map[string]*widget.Clickable)
	}
	if ui.classicOEJumpClicks == nil {
		ui.classicOEJumpClicks = make(map[string]*widget.Clickable)
	}
	if ui.classicOEExpandOpen == nil {
		ui.classicOEExpandOpen = make(map[string]bool)
	}
}

// layoutClassicOEAnimTree is the body of the Anims / Motions / Actions
// header: the four folders with their counts, each toggling its contents
// inline, so the whole panel reads as the editor's tree rather than a plain
// animation list.
func (ui *uiState) layoutClassicOEAnimTree(gtx layout.Context) layout.Dimensions {
	ui.ensureClassicOEFolders()
	v := ui.unitModelViewer()
	doc, docMsg := ui.classicOEListDoc()
	var timelines []whe.FxTimeline
	var actions []whe.FxAction
	if doc != nil {
		timelines = doc.Timelines()
		actions = doc.Actions()
	}
	roots := classicOETreeRoots(actions)
	counts := [classicOEFolderCount]int{}
	if v != nil && v.model != nil {
		counts[classicOEFolderAnims] = len(v.model.Animations)
	}
	counts[classicOEFolderMotions] = len(timelines)
	counts[classicOEFolderActions] = len(actions)
	counts[classicOEFolderTree] = len(roots)
	labels := [classicOEFolderCount]string{"Animations", "Motions", "Actions", "Action Tree"}

	var children []layout.FlexChild
	for folder := 0; folder < classicOEFolderCount; folder++ {
		folder := folder
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.layoutClassicOEFolderRow(gtx, folder, labels[folder], counts[folder])
		}))
		if !ui.classicOEFolderOpen[folder] {
			continue
		}
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.layoutClassicOEFolderBody(gtx, folder, v, timelines, actions, docMsg)
		}))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}

// layoutClassicOEFolderRow is one folder header: marker, name, count, and
// the whole row is the toggle. The count stays visible while closed so that
// a folder still says what it holds.
func (ui *uiState) layoutClassicOEFolderRow(gtx layout.Context, folder int, label string, count int) layout.Dimensions {
	ui.ensureClassicOEFolders()
	c := &ui.classicOEFolderClicks[folder]
	if c.Clicked(gtx) {
		ui.classicOEFolderOpen[folder] = !ui.classicOEFolderOpen[folder]
	}
	open := ui.classicOEFolderOpen[folder]
	bg := cPanelBg
	if open {
		bg = cHeaderBg
	} else if c.Hovered() {
		bg = cMenuHover
	}
	return layout.Inset{Top: unit.Dp(1), Bottom: unit.Dp(1), Left: unit.Dp(4), Right: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return c.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			h := gtx.Dp(unit.Dp(21))
			// The bar spans the column's width, so the size is pinned
			// before the stack paints: with no minimum the background
			// would fall back to the row's content width.
			gtx.Constraints.Min = image.Pt(gtx.Constraints.Max.X, h)
			gtx.Constraints.Max = image.Pt(gtx.Constraints.Max.X, h)
			return layout.Stack{Alignment: layout.Center}.Layout(gtx,
				layout.Expanded(func(gtx layout.Context) layout.Dimensions {
					rrect(gtx, bg, gtx.Constraints.Max, gtx.Dp(2))
					return layout.Dimensions{Size: gtx.Constraints.Max}
				}),
				layout.Stacked(func(gtx layout.Context) layout.Dimensions {
					marker := "▸"
					if open {
						marker = "▾"
					}
					return layout.Inset{Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								l := material.Label(ui.th, unit.Sp(9), marker)
								l.Color = cTextDim
								return l.Layout(gtx)
							}),
							layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								l := material.Label(ui.th, unit.Sp(11), label)
								l.Color = cText
								l.MaxLines = 1
								return l.Layout(gtx)
							}),
							layout.Flexed(1, rowGap()),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								l := material.Label(ui.th, unit.Sp(9), fmt.Sprintf("(%d)", count))
								l.Color = cTextDim
								return l.Layout(gtx)
							}),
						)
					})
				}),
			)
		})
	})
}

// layoutClassicOEFolderBody renders what one open folder holds: the clip
// list, the motion rows, the action rows, or the derived action tree.
func (ui *uiState) layoutClassicOEFolderBody(gtx layout.Context, folder int, v *modelViewer, timelines []whe.FxTimeline, actions []whe.FxAction, docMsg string) layout.Dimensions {
	switch folder {
	case classicOEFolderAnims:
		return ui.layoutClassicOEAnimList(gtx)
	case classicOEFolderMotions:
		if len(timelines) == 0 {
			return ui.layoutClassicOEHint(gtx, docMsg, "This .whe defines no motions.")
		}
		if len(ui.classicOEMotionClicks) != len(timelines) {
			ui.classicOEMotionClicks = make([]widget.Clickable, len(timelines))
		}
		children := make([]layout.FlexChild, 0, len(timelines))
		for i := range timelines {
			t := timelines[i]
			c := &ui.classicOEMotionClicks[i]
			if c.Clicked(gtx) {
				// A motion is named for the animation it drives, so
				// one press jumps exactly as the tree's leaf does —
				// and a double press opens the timeline's editor in
				// the dialog instead.
				if ui.classicOEDoubleClick("motion:" + strings.ToLower(t.Name)) {
					ui.openClassicOEEventDialog(classicOEDialogTimeline, t.Name, 0)
				} else {
					ui.classicOETreeActivate(classicOETreeRow{Name: t.Name})
				}
			}
			note := classicOEUseNote(len(t.Events), "event", "events")
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.unitViewportRow(gtx, c, false, t.Name, note)
			}))
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	case classicOEFolderActions:
		if len(actions) == 0 {
			return ui.layoutClassicOEHint(gtx, docMsg, "This .whe defines no actions.")
		}
		if len(ui.classicOEActionClicks) != len(actions) {
			ui.classicOEActionClicks = make([]widget.Clickable, len(actions))
		}
		children := make([]layout.FlexChild, 0, len(actions))
		for i := range actions {
			a := actions[i]
			c := &ui.classicOEActionClicks[i]
			if c.Clicked(gtx) {
				// One press jumps to the action's clip — the same
				// resolution the Action Tree's branch rows use —
				// and a double press opens its condition editor in
				// the dialog instead.
				if ui.classicOEDoubleClick("action:" + strings.ToLower(a.Name)) {
					ui.openClassicOEEventDialog(classicOEDialogAction, a.Name, i)
				} else {
					key := "/" + strings.ToLower(strings.TrimSpace(a.Name))
					ui.classicOETreeActivate(classicOETreeRow{Name: a.Name, Key: key})
				}
			}
			note := classicOEUseNote(len(a.Motions)+len(a.Subactions), "choice", "choices")
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.unitViewportRow(gtx, c, false, a.Name, note)
			}))
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	case classicOEFolderTree:
		if len(classicOETreeRoots(actions)) == 0 {
			return ui.layoutClassicOEHint(gtx, docMsg, "This .whe defines no actions to build a tree from.")
		}
		rows := classicOETreeRows(actions, ui.classicOEExpandOpen)
		children := make([]layout.FlexChild, 0, len(rows))
		for i := range rows {
			row := rows[i]
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.layoutClassicOETreeRow(gtx, row)
			}))
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	}
	return layout.Dimensions{}
}

// layoutClassicOEHint is an empty folder's one line: the reason the .whe is
// missing when there is one, otherwise the folder's own emptiness.
func (ui *uiState) layoutClassicOEHint(gtx layout.Context, docMsg, fallback string) layout.Dimensions {
	msg := fallback
	if docMsg != "" {
		msg = docMsg
	}
	l := material.Label(ui.th, unit.Sp(9), msg)
	l.Color = cTextDim
	return layout.Inset{Top: unit.Dp(2), Bottom: unit.Dp(4), Left: unit.Dp(10), Right: unit.Dp(8)}.Layout(gtx, l.Layout)
}

// layoutClassicOETreeRow is one row of the Action Tree: indentation by
// depth, the expand marker where the node has children, the name, and the
// condition dimmed beside it. The row carries two separate press targets —
// the marker column toggles a branch open, everything else plays the
// animation the node points at — so navigation and expansion never fight
// over one press. A leaf's blank marker column belongs to the jump target,
// which keeps the whole line live.
func (ui *uiState) layoutClassicOETreeRow(gtx layout.Context, row classicOETreeRow) layout.Dimensions {
	ui.ensureClassicOEFolders()
	indent := unit.Dp(8 + row.Depth*10)

	// A map index cannot be addressed, so each node's press targets are
	// allocated once and kept beside the expansion state.
	var expand *widget.Clickable
	if row.Key != "" {
		expand = ui.classicOEExpandClicks[row.Key]
		if expand == nil {
			expand = &widget.Clickable{}
			ui.classicOEExpandClicks[row.Key] = expand
		}
		if expand.Clicked(gtx) {
			// Toggle from the same default the derivation used, so a
			// key the map has never seen flips to its opposite once.
			ui.classicOEExpandOpen[row.Key] = !row.Expanded
		}
	}
	// The jump target is keyed by node path for a branch — its subtree
	// decides the clip — and by name for a leaf: two leaves of one name
	// play the same clip anyway.
	jumpKey := "\x00leaf:" + strings.ToLower(strings.TrimSpace(row.Name))
	if row.Key != "" {
		jumpKey = row.Key
	}
	jump := ui.classicOEJumpClicks[jumpKey]
	if jump == nil {
		jump = &widget.Clickable{}
		ui.classicOEJumpClicks[jumpKey] = jump
	}
	if jump.Clicked(gtx) {
		ui.classicOETreeActivate(row)
	}

	hover := jump.Hovered() || (expand != nil && expand.Hovered())
	content := func(gtx layout.Context) layout.Dimensions {
		// Pinned like the folder bar: the hover background has to span
		// the whole row, not the name's width.
		h := gtx.Dp(unit.Dp(18))
		gtx.Constraints.Min = image.Pt(gtx.Constraints.Max.X, h)
		gtx.Constraints.Max = image.Pt(gtx.Constraints.Max.X, h)
		return layout.Stack{Alignment: layout.Center}.Layout(gtx,
			layout.Expanded(func(gtx layout.Context) layout.Dimensions {
				if hover {
					rrect(gtx, cMenuHover, gtx.Constraints.Max, gtx.Dp(2))
				}
				return layout.Dimensions{Size: gtx.Constraints.Max}
			}),
			layout.Stacked(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(1), Bottom: unit.Dp(1), Left: indent, Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							marker := ""
							if row.Key != "" {
								marker = "▸"
								if row.Expanded {
									marker = "▾"
								}
							}
							gtx.Constraints.Min.X = gtx.Dp(unit.Dp(14))
							gtx.Constraints.Max.X = gtx.Dp(unit.Dp(14))
							gtx.Constraints.Min.Y = gtx.Constraints.Max.Y
							draw := func(gtx layout.Context) layout.Dimensions {
								return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									l := material.Label(ui.th, unit.Sp(9), marker)
									l.Color = cTextDim
									return l.Layout(gtx)
								})
							}
							if expand != nil {
								return expand.Layout(gtx, draw)
							}
							// A leaf's blank marker is part of the jump
							// target, so no part of the line is dead.
							return jump.Layout(gtx, draw)
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							return jump.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								gtx.Constraints.Min.Y = gtx.Constraints.Max.Y
								return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										l := material.Label(ui.th, unit.Sp(10), row.Name)
										l.Color = cText
										return l.Layout(gtx)
									}),
									layout.Flexed(1, rowGap()),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										if row.Note == "" {
											return layout.Dimensions{}
										}
										l := material.Label(ui.th, unit.Sp(9), classicOENote(row.Note))
										l.Color = cTextDim
										l.MaxLines = 1
										return l.Layout(gtx)
									}),
								)
							})
						}),
					)
				})
			}),
		)
	}
	return content(gtx)
}

// classicOETreeActivate plays the clip a pressed Action Tree row points at
// in the preview beside the panel — the same selection and playback the
// Animations folder's rows perform — and says so in the status bar when the
// model carries no such clip.
func (ui *uiState) classicOETreeActivate(row classicOETreeRow) {
	v := ui.unitModelViewer()
	if v == nil || v.model == nil {
		ui.statusText = "Action Tree — the model is still loading"
		return
	}
	clips := make([]string, len(v.model.Animations))
	for i := range v.model.Animations {
		clips[i] = v.model.Animations[i].Name
	}
	var actions []whe.FxAction
	if doc, _ := ui.classicOEListDoc(); doc != nil {
		actions = doc.Actions()
	}
	idx := classicOETreeJumpIndex(row, actions, clips)
	if idx < 0 {
		ui.statusText = fmt.Sprintf("Action Tree — no animation named %q in this model", row.Name)
		return
	}
	v.selectAnim(idx)
	v.playing = true
	v.lastFrame = time.Time{}
}

// classicOETreeJumpIndex picks the clip a pressed Action Tree row plays.
// A motion is named for the animation it drives, so an exact match on the
// row's own name is the normal path; an action's name is structure rather
// than a clip ("die", "movefire"), so a branch falls back to the first clip
// its subtree references — the children in the order the tree draws them:
// each subaction first, then the action's own motions. Names compare
// case-insensitively; -1 means the model carries no matching animation.
func classicOETreeJumpIndex(row classicOETreeRow, actions []whe.FxAction, clips []string) int {
	match := func(name string) int {
		name = strings.ToLower(strings.TrimSpace(name))
		for i, clip := range clips {
			if strings.ToLower(strings.TrimSpace(clip)) == name {
				return i
			}
		}
		return -1
	}
	if i := match(row.Name); i >= 0 {
		return i
	}
	if row.Key == "" || len(actions) == 0 {
		return -1
	}
	byName := make(map[string]*whe.FxAction, len(actions))
	for i := range actions {
		byName[strings.ToLower(strings.TrimSpace(actions[i].Name))] = &actions[i]
	}
	// The key is the lowercased action names of the chain from a root to
	// this node, exactly as the derivation stored them.
	chain := strings.Split(strings.Trim(row.Key, "/"), "/")
	var target *whe.FxAction
	if len(chain) > 0 {
		target = byName[chain[0]]
	}
	for i := 1; target != nil && i < len(chain); i++ {
		var next *whe.FxAction
		for _, ln := range target.Subactions {
			name := strings.ToLower(strings.TrimSpace(ln.Name))
			if name == chain[i] {
				if t, ok := byName[name]; ok {
					next = t
					break
				}
			}
		}
		target = next
	}
	if target == nil {
		return -1
	}
	seen := make(map[string]bool)
	var search func(a *whe.FxAction, depth int) int
	search = func(a *whe.FxAction, depth int) int {
		name := strings.ToLower(strings.TrimSpace(a.Name))
		if seen[name] || depth > classicOETreeMaxDepth {
			return -1
		}
		seen[name] = true
		for _, ln := range a.Subactions {
			child, ok := byName[strings.ToLower(strings.TrimSpace(ln.Name))]
			if !ok {
				continue
			}
			if i := search(child, depth+1); i >= 0 {
				return i
			}
		}
		for _, ln := range a.Motions {
			if i := match(ln.Name); i >= 0 {
				return i
			}
		}
		return -1
	}
	return search(target, 0)
}

// ── the double-click dialog ─────────────────────────────────────────────────
//
// A double press on a Motions or Actions row opens a small dialog with that
// item's editor. The dialog carries no editing logic of its own: it is a
// second surface on the Events section's own state — the same document, the
// same fields, the same Save .whe — so the two surfaces cannot drift apart.

const (
	classicOEDialogNone = iota
	classicOEDialogTimeline
	classicOEDialogAction
)

// classicOEDoubleClickDelay is the window the mod tree's file rows use to
// read two presses as one double press.
const classicOEDoubleClickDelay = 350 * time.Millisecond

// classicOEDialogOpen reports whether the dialog is on screen; while it is,
// the Events editors it embeds are live even though Classic OE is the
// section showing.
func (ui *uiState) classicOEDialogOpen() bool {
	return ui.classicOEDialog != classicOEDialogNone
}

// classicOEDoubleClick records a press on key and reports whether it repeats
// the previous one inside the window. A double consumes the record, so a
// third press counts as a fresh first one.
func (ui *uiState) classicOEDoubleClick(key string) bool {
	now := time.Now()
	if ui.classicOELastClickKey == key && now.Sub(ui.classicOELastClickAt) < classicOEDoubleClickDelay {
		ui.classicOELastClickKey = ""
		return true
	}
	ui.classicOELastClickKey = key
	ui.classicOELastClickAt = now
	return false
}

// openClassicOEEventDialog raises the dialog on the clicked item by pointing
// the shared Events editor at it: the picked timeline for a motion, the
// picked action for an action.
func (ui *uiState) openClassicOEEventDialog(kind int, name string, actionIndex int) {
	ui.ensureUnitEvents()
	ui.classicOEDialog = kind
	ui.classicOEDialogName = name
	if kind == classicOEDialogAction {
		// The same switch the Events section's action rows make: the panel
		// shows the action's conditions and none of the timeline's events.
		if !ui.unitEvtFocusAction {
			ui.evtPlayer.stopAll()
		}
		ui.unitEvtFocusAction = true
		if ui.unitEvtDoc != nil {
			if acts := ui.unitEvtDoc.Actions(); actionIndex >= 0 && actionIndex < len(acts) {
				ui.unitEvtActionSel = actionIndex
			}
		}
		return
	}
	ui.unitEvtFocusAction = false
	ui.selectUnitEventTimeline(name)
}

// layoutClassicOEEventDialog is the dialog itself: a dimmed card over the
// Classic OE panel with the Events section's own editor body and footer
// inside. A press on the dim or the Close button shuts it; Save .whe and
// Revert act on the same document the Events section would act on.
func (ui *uiState) layoutClassicOEEventDialog(gtx layout.Context) layout.Dimensions {
	if ui.classicOEevtClose.Clicked(gtx) || ui.classicOEevtDim.Clicked(gtx) {
		ui.classicOEDialog = classicOEDialogNone
		return layout.Dimensions{}
	}
	title := "Timeline editor"
	if ui.classicOEDialog == classicOEDialogAction {
		title = "Action editor"
	}
	sub := ui.classicOEDialogName
	if ui.unitEvtDirty {
		sub += "  \u2022 unsaved"
	}
	body := func(gtx layout.Context) layout.Dimensions {
		// The action body reads straight from the document, so a file that
		// never loaded stays on the guarded timeline body, which carries
		// the reason instead of a nil dereference.
		if ui.classicOEDialog == classicOEDialogAction && ui.unitEvtDoc != nil {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx, ui.unitEvtActionPanelBody(gtx)...)
		}
		return ui.unitEventPanelBody(gtx)
	}
	footer := func(gtx layout.Context) layout.Dimensions {
		if ui.classicOEDialog == classicOEDialogAction {
			return ui.unitActPanelFooter(gtx)
		}
		return ui.unitEventPanelForm(gtx)
	}
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			// The dim is the dialog's own click target: a press on it
			// closes the card and never reaches the panel underneath.
			return ui.classicOEevtDim.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				sz := gtx.Constraints.Max
				st := clip.Rect{Max: sz}.Push(gtx.Ops)
				paint.ColorOp{Color: rgba(0, 0, 0, 70)}.Add(gtx.Ops)
				paint.PaintOp{}.Add(gtx.Ops)
				st.Pop()
				return layout.Dimensions{Size: sz}
			})
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				cardW := minInt(gtx.Dp(unit.Dp(700)), gtx.Constraints.Max.X-gtx.Dp(unit.Dp(30)))
				cardH := minInt(gtx.Dp(unit.Dp(680)), gtx.Constraints.Max.Y-gtx.Dp(unit.Dp(30)))
				gtx.Constraints.Min = image.Pt(cardW, cardH)
				gtx.Constraints.Max = image.Pt(cardW, cardH)
				return widget.Border{Color: cBorder, Width: unit.Dp(1), CornerRadius: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Stack{}.Layout(gtx,
						layout.Expanded(func(gtx layout.Context) layout.Dimensions {
							rrect(gtx, cPanelBg, gtx.Constraints.Max, gtx.Dp(unit.Dp(4)))
							return layout.Dimensions{Size: gtx.Constraints.Max}
						}),
						layout.Stacked(func(gtx layout.Context) layout.Dimensions {
							inset := layout.Inset{Top: unit.Dp(12), Bottom: unit.Dp(10), Left: unit.Dp(12), Right: unit.Dp(12)}
							return inset.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												l := material.Label(ui.th, unit.Sp(12), title)
												l.Color = cText
												return l.Layout(gtx)
											}),
											layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												l := material.Label(ui.th, unit.Sp(9), sub)
												l.Color = cTextDim
												return l.Layout(gtx)
											}),
											layout.Flexed(1, rowGap()),
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												return ui.smallButton(gtx, &ui.classicOEevtClose, "Close")
											}),
										)
									}),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, cHeaderLine, 1) }),
									layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
										return ui.panelScrollView(gtx, &ui.classicOEevtScroll, body)
									}),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, cHeaderLine, 1) }),
									layout.Rigid(footer),
								)
							})
						}),
					)
				})
			})
		}),
	)
}

// classicOETreeRoots is the actions no other action calls — the roots the
// classic editor's Action Tree hung from. A called sub-action lives under
// its caller instead, which is what turns the flat list of DATAACTS records
// into the nested tree the editor drew.
func classicOETreeRoots(actions []whe.FxAction) []whe.FxAction {
	called := make(map[string]bool, len(actions))
	for _, a := range actions {
		for _, ln := range a.Subactions {
			called[strings.ToLower(strings.TrimSpace(ln.Name))] = true
		}
	}
	roots := make([]whe.FxAction, 0, len(actions))
	for _, a := range actions {
		if !called[strings.ToLower(strings.TrimSpace(a.Name))] {
			roots = append(roots, a)
		}
	}
	return roots
}

// classicOETreeRow is one row of the Action Tree folder.
type classicOETreeRow struct {
	Name     string
	Depth    int
	Note     string // condition of the incoming subaction link
	Key      string // expansion key; "" for a leaf
	Expanded bool   // the default or current state, used by the toggle
}

// classicOETreeRows flattens the derived action tree depth-first: each
// action under its caller, then that action's own motions as leaves. The
// expansion state is consulted per node, with roots open by default and
// nested nodes closed, mirroring how the editor opened the tree.
func classicOETreeRows(actions []whe.FxAction, open map[string]bool) []classicOETreeRow {
	byName := make(map[string]*whe.FxAction, len(actions))
	for i := range actions {
		byName[strings.ToLower(strings.TrimSpace(actions[i].Name))] = &actions[i]
	}
	var rows []classicOETreeRow
	var walk func(a *whe.FxAction, depth int, path, note string, onPath map[string]bool)
	walk = func(a *whe.FxAction, depth int, path, note string, onPath map[string]bool) {
		name := strings.ToLower(strings.TrimSpace(a.Name))
		key := path + "/" + name
		row := classicOETreeRow{Name: a.Name, Depth: depth, Note: note, Key: key}
		if v, ok := open[key]; ok {
			row.Expanded = v
		} else {
			row.Expanded = path == "" // roots open, nested nodes closed
		}
		rows = append(rows, row)
		if !row.Expanded || depth >= classicOETreeMaxDepth {
			return
		}
		childPath := make(map[string]bool, len(onPath)+1)
		for k := range onPath {
			childPath[k] = onPath[k]
		}
		childPath[name] = true
		for _, ln := range a.Subactions {
			childName := strings.ToLower(strings.TrimSpace(ln.Name))
			childNote := ""
			if ln.HasCondition() && ln.Condition != "" {
				childNote = ln.Guard()
			}
			target, ok := byName[childName]
			if ok && childPath[childName] {
				// A link back up the tree: show the name as an inert
				// leaf rather than recursing into it again.
				rows = append(rows, classicOETreeRow{Name: ln.Name, Depth: depth + 1, Note: childNote})
				continue
			}
			if !ok {
				// A subaction with no record of its own (a .whe typo
				// or an engine built-in): still show the name.
				rows = append(rows, classicOETreeRow{Name: ln.Name, Depth: depth + 1, Note: childNote})
				continue
			}
			walk(target, depth+1, key, childNote, childPath)
		}
		for _, ln := range a.Motions {
			motionNote := ""
			if ln.HasCondition() && ln.Condition != "" {
				motionNote = ln.Guard()
			}
			rows = append(rows, classicOETreeRow{Name: ln.Name, Depth: depth + 1, Note: motionNote})
		}
	}
	for _, root := range classicOETreeRoots(actions) {
		if target, ok := byName[strings.ToLower(strings.TrimSpace(root.Name))]; ok {
			walk(target, 0, "", "", nil)
		}
	}
	return rows
}

// layoutClassicOEEventList is the Event List panel: every event definition of
// the unit's .whe, sorted, with the effect file it plays beside it — the same
// document the Events section edits, read the same way.
func (ui *uiState) layoutClassicOEEventList(gtx layout.Context) layout.Dimensions {
	doc, msg := ui.classicOEListDoc()
	if doc == nil {
		return classicOEHint(gtx, ui.th, msg)
	}
	events := doc.Events()
	if len(events) == 0 {
		return classicOEHint(gtx, ui.th, "This .whe defines no events.")
	}
	names := make([]string, 0, len(events))
	for name := range events {
		names = append(names, name)
	}
	sort.Strings(names)
	var rows []layout.FlexChild
	for _, name := range names {
		name := name
		note := classicOEUseNote(len(events[name].Entries), "entry", "entries")
		if fx, ok := events[name].First("fx"); ok {
			note = classicOENote(fx)
		}
		rows = append(rows, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.classicOEStaticRow(gtx, name, note)
		}))
	}
	return classicOERows(gtx, rows)
}

// layoutClassicOEConditionList is the Condition List panel: every distinct
// condition the .whe's actions guard their choices with, sorted, with how
// many choices test it.
func (ui *uiState) layoutClassicOEConditionList(gtx layout.Context) layout.Dimensions {
	doc, msg := ui.classicOEListDoc()
	if doc == nil {
		return classicOEHint(gtx, ui.th, msg)
	}
	counts := make(map[string]int)
	for _, act := range doc.Actions() {
		for _, links := range [][]whe.FxActionLink{act.Motions, act.Subactions} {
			for _, ln := range links {
				cond := strings.TrimSpace(ln.Condition)
				if ln.HasCondition() && cond != "" {
					counts[cond]++
				}
			}
		}
	}
	if len(counts) == 0 {
		return classicOEHint(gtx, ui.th, "This .whe's actions carry no conditions.")
	}
	return ui.classicOENamedCountRows(gtx, counts, "use", "uses")
}

// layoutClassicOEModifierList is the Modifier List panel: every distinct
// modifier the .whe's motions drive, sorted, with how many motions drive it.
// The modifier name lives in a timeline's motion tail, which is the only
// place the .whe records one.
func (ui *uiState) layoutClassicOEModifierList(gtx layout.Context) layout.Dimensions {
	doc, msg := ui.classicOEListDoc()
	if doc == nil {
		return classicOEHint(gtx, ui.th, msg)
	}
	counts := make(map[string]int)
	for _, tl := range doc.Timelines() {
		if mod := strings.TrimSpace(tl.Tail.Modifier); mod != "" {
			counts[mod]++
		}
	}
	if len(counts) == 0 {
		return classicOEHint(gtx, ui.th, "No motion of this .whe drives a modifier.")
	}
	return ui.classicOENamedCountRows(gtx, counts, "motion", "motions")
}

// classicOENamedCountRows is the shared body of the condition and modifier
// panels: one row per name, alphabetically, with a count beside it.
func (ui *uiState) classicOENamedCountRows(gtx layout.Context, counts map[string]int, one, many string) layout.Dimensions {
	names := make([]string, 0, len(counts))
	for name := range counts {
		names = append(names, name)
	}
	sort.Strings(names)
	var rows []layout.FlexChild
	for _, name := range names {
		name := name
		note := classicOEUseNote(counts[name], one, many)
		rows = append(rows, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.classicOEStaticRow(gtx, name, note)
		}))
	}
	return classicOERows(gtx, rows)
}

// layoutClassicOEPreview is the grey area of the classic editor, filled with
// the unit's model: the same canvas the Viewport section draws, so camera
// drags, playback and the scrub bar all work here without a second viewer.
func (ui *uiState) layoutClassicOEPreview(gtx layout.Context) layout.Dimensions {
	return panelFrame(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.layoutUnitColumnHeader(gtx, "Preview")
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, cHeaderLine, 1) }),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(6), Left: unit.Dp(6), Right: unit.Dp(6), Bottom: unit.Dp(6)}.Layout(gtx, ui.layoutUnitModelCanvas)
			}),
		)
	})
}
