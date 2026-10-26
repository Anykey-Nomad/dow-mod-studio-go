package app

import (
	"fmt"
	"image"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"dow-mod-studio/internal/3d_engine"
	"dow-mod-studio/internal/whe"
	"gioui.org/io/key"
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
	classicOEKindClauses    = "clauses"
	classicOEKindModifiers  = "modifiers"
	classicOEKindMeshes     = "meshes"
	classicOEKindSelectedUI = "selected-ui"
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
	{"Selected UI", classicOEKindSelectedUI, ""},
	{"Condition List", classicOEKindConditions, ""},
	{"Clause List", classicOEKindClauses, ""},
	{"Event List", classicOEKindEvents, ""},
	{"Modifier List", classicOEKindModifiers, ""},
	{"Anims / Motions / Actions", classicOEKindAnims, ""},
	{"Meshes", classicOEKindMeshes, ""},
}

// classicOEAnimsHeader is the index of the anims panel: the one whose
// expanded body is the model's clip list and which starts open. It is found
// by kind rather than by position, so adding or reordering panels cannot
// silently point the default at the wrong one.
var classicOEAnimsHeader = func() int {
	for i, h := range classicOEHeaders {
		if h.kind == classicOEKindAnims {
			return i
		}
	}
	return len(classicOEHeaders) - 1
}()

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
// preview with its playback row underneath it, then the same list panel the
// Viewport section draws, on the right — the list tabs and the preview toggles
// of the unit's model. The playback row belongs to the preview alone rather
// than to the three columns, so nothing under the panel column or the list is
// covered by controls that do not drive it. The model load is kicked off
// here the same way the Viewport section kicks it off, so opening the section
// on a unit that has not been previewed yet still ends with a model on screen.
func (ui *uiState) layoutClassicOEArea(gtx layout.Context) layout.Dimensions {
	ui.ensureUnitModel(gtx)
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
			sideW := gtx.Dp(unit.Dp(unitViewportSideDp))
			gap := gtx.Dp(unit.Dp(unitColGapDp))
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx, layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
					layout.Rigid(ui.fixedPanel(panelW, ui.layoutClassicOELeftPanel)),
					layout.Rigid(layout.Spacer{Width: unit.Dp(gap)}.Layout),
					layout.Flexed(1, ui.layoutClassicOEPreview),
					layout.Rigid(layout.Spacer{Width: unit.Dp(gap)}.Layout),
					layout.Rigid(ui.fixedPanel(sideW, ui.layoutUnitModelSide)),
				)
			}),
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
		return ui.layoutClassicOEConditionEditorList(gtx)
	case classicOEKindClauses:
		return ui.layoutClassicOEClauseList(gtx)
	case classicOEKindModifiers:
		return ui.layoutClassicOEModifierList(gtx)
	case classicOEKindMeshes:
		return ui.unitMeshRows(gtx)
	case classicOEKindSelectedUI:
		return ui.layoutClassicOESelectedUI(gtx)
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
				// Every action row reserves the icon column — the game's
				// own command picture where the name says what the action
				// is, blank space where it does not — so the names stay
				// in one line either way, as the unit list does.
				return ui.unitViewportRowIcon(gtx, c, false, a.Name, note, ui.actionIcon(a.Name), true)
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
		// A second press opens the row's editor — an action's condition
		// editor, a motion leaf's timeline editor — the way the Actions
		// and Motions folders do; the first press has already played it.
		if !ui.classicOETreeRowOpensDialog(row) {
			ui.classicOETreeActivate(row)
		}
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
										// Branch rows are actions: they carry a folder
										// to say the row opens into a list, then the
										// same small command icon the Actions folder
										// shows. A motion leaf has neither, so the
										// column belongs to branches alone.
										if row.Key == "" {
											return layout.Dimensions{}
										}
										return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												return layout.Inset{Right: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
													return folderIconLayout(gtx, 14, 11)
												})
											}),
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												return layout.Inset{Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
													return unitIconLayout(gtx, ui.actionIcon(row.Name), 20, 15)
												})
											}),
										)
									}),
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
// model carries no such clip. A row named for an action plays that whole
// action: every clip its graph resolves to, combined in one pose.
func (ui *uiState) classicOETreeActivate(row classicOETreeRow) {
	v := ui.unitModelViewer()
	if v == nil || v.model == nil {
		ui.statusText = "Action Tree — the model is still loading"
		return
	}
	// The action graph comes first: an action or motion is often named for
	// none of the model's clips — only for the timelines that lead to them —
	// and the exact-name jump below then found nothing to play.
	if ui.activateUnitAction(row.Name) {
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

// classicOETreeRowOpensDialog reads a press on an Action Tree row for a
// double: the second press on an action row raises its condition editor and
// the second press on a motion leaf raises its timeline editor — the same
// dialogs the Actions and Motions folders raise, each duplicating its area
// of the Events tab. It answers false for a first press, so the caller plays
// the row instead, and true when the press was a double — whether or not the
// row turned out to have an editor.
func (ui *uiState) classicOETreeRowOpensDialog(row classicOETreeRow) bool {
	name := strings.ToLower(strings.TrimSpace(row.Name))
	key := "motion:" + name
	if row.Key != "" {
		// Branches are actions; a leaf's Key is empty.
		key = "action:" + name
	}
	if !ui.classicOEDoubleClick(key) {
		return false
	}
	// The double belongs to this row: open its editor when it has one.
	doc, _ := ui.classicOEListDoc()
	if doc == nil {
		return true
	}
	kind, itemName, actionIndex, ok := classicOETreeRowDialog(doc, row)
	if ok {
		ui.openClassicOEEventDialog(kind, itemName, actionIndex)
	}
	return true
}

// classicOETreeRowDialog names the editor a double press on the row raises:
// an action row opens the action's condition editor, a motion leaf the
// timeline of the same name. ok is false when the row's name is neither —
// an unrecorded link or a motion the file carries no timeline for, which
// have no editor to open.
func classicOETreeRowDialog(doc *whe.Document, row classicOETreeRow) (kind int, name string, actionIndex int, ok bool) {
	if doc == nil {
		return classicOEDialogNone, "", -1, false
	}
	if row.Key != "" {
		for i, action := range doc.Actions() {
			if strings.EqualFold(strings.TrimSpace(action.Name), strings.TrimSpace(row.Name)) {
				return classicOEDialogAction, action.Name, i, true
			}
		}
		return classicOEDialogNone, "", -1, false
	}
	for _, timeline := range doc.Timelines() {
		if strings.EqualFold(strings.TrimSpace(timeline.Name), strings.TrimSpace(row.Name)) {
			return classicOEDialogTimeline, timeline.Name, 0, true
		}
	}
	return classicOEDialogNone, "", -1, false
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
// Revert act on the same document the Events section would act on. A press on
// the card's own empty space is neither: the card takes it, so clicking the
// whitespace of the editor no longer closes it.
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
							// The card takes the press before the dim beneath can
							// have it: the editor rarely fills the card, and a
							// click on what is left used to shut the dialog.
							absorbDialogCardPress(gtx, &ui.classicOEevtCardTag)
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
	if len(roots) == 0 && len(actions) > 0 {
		// Every action is called — the graph is one closed cycle. Start the
		// tree at the first action so a cyclic .whe still draws a tree
		// instead of reporting that it has none.
		roots = append(roots, actions[0])
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
	// linkNote is the guard a link carries, drawn beside the row it hangs
	// under: "if cond", "else if cond" or "else" — an unguarded link has
	// nothing to say, and a guard missing its condition says nothing usefully.
	linkNote := func(ln whe.FxActionLink) string {
		switch {
		case ln.HasCondition() && ln.Condition != "":
			return ln.Guard()
		case ln.Compare == whe.FxCompareElse:
			return ln.Guard()
		}
		return ""
	}
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
			childNote := linkNote(ln)
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
			motionNote := linkNote(ln)
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
	mods := doc.Modifiers()
	if len(mods) == 0 {
		return classicOEHint(gtx, ui.th, "This .whe defines no modifiers.")
	}
	if len(ui.classicOEModifierClicks) != len(mods) {
		ui.classicOEModifierClicks = make([]widget.Clickable, len(mods))
	}
	var rows []layout.FlexChild
	for i := range mods {
		i := i
		m := mods[i]
		rows = append(rows, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.classicOEModifierRow(gtx, i, m)
		}))
	}
	rows = append(rows, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return ui.layoutClassicOEModifierEditor(gtx)
	}))
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, rows...)
}

func (ui *uiState) classicOEModifierRow(gtx layout.Context, i int, m whe.FxModifier) layout.Dimensions {
	if i < 0 || i >= len(ui.classicOEModifierClicks) {
		return layout.Dimensions{}
	}
	c := &ui.classicOEModifierClicks[i]
	if c.Clicked(gtx) {
		ui.classicOEModifierName = m.Name
		ui.classicOEModifierBound = ""
		ui.classicOEModifierError = ""
	}
	bg := cPanelBg
	if ui.classicOEModifierName == m.Name {
		bg = cSelectBg
	} else if c.Hovered() {
		bg = cMenuHover
	}
	return c.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		h := gtx.Dp(unit.Dp(22))
		sz := image.Pt(gtx.Constraints.Max.X, h)
		gtx.Constraints.Min, gtx.Constraints.Max = sz, sz
		return layout.Stack{}.Layout(gtx,
			layout.Expanded(func(gtx layout.Context) layout.Dimensions {
				rrect(gtx, bg, sz, gtx.Dp(2))
				return layout.Dimensions{Size: sz}
			}),
			layout.Stacked(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.th, unit.Sp(10), m.Name)
							l.Color = cText
							l.MaxLines = 1
							return l.Layout(gtx)
						}),
						layout.Flexed(1, rowGap()),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.th, unit.Sp(9), whe.ModifierTypeName(m.Type))
							l.Color = cTextDim
							l.MaxLines = 1
							return l.Layout(gtx)
						}),
					)
				})
			}),
		)
	})
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
// layoutClassicOEPreview is the middle column: the model, with the playback
// row under it — the same row the Viewport section draws under its own canvas,
// and only as wide as the preview above it, so neither the panel column nor
// the list panel is covered by controls that do not belong to them.
func (ui *uiState) layoutClassicOEPreview(gtx layout.Context) layout.Dimensions {
	return panelFrame(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.layoutUnitColumnHeader(gtx, "Preview")
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, cHeaderLine, 1) }),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(6), Left: unit.Dp(6), Right: unit.Dp(6), Bottom: unit.Dp(6)}.Layout(gtx, ui.layoutUnitCanvasColumn)
			}),
		)
	})
}

// ─── merged from unit_classic_oe_clauses_ui.go ───────────────────────────────────────────────

func (ui *uiState) ensureClassicOEClause() (whe.FxClause, bool) {
	if ui.unitEvtDoc == nil {
		return whe.FxClause{}, false
	}
	for _, clause := range ui.unitEvtDoc.Clauses() {
		if strings.EqualFold(clause.Name, ui.classicOEClauseName) {
			texts := clauseEditorValues(clause)
			bound := ui.unitEvtPath + "|" + clause.Name + "|" + strings.Join(texts[:], "|")
			if bound != ui.classicOEClauseBound {
				ui.classicOEClauseBound = bound
				ui.classicOEClauseError = ""
				for i, text := range texts {
					ui.classicOEClauseEditors[i].SetText(text)
					ui.classicOEClauseFill[i] = text
				}
			}
			return clause, true
		}
	}
	return whe.FxClause{}, false
}

func clauseEditorValues(c whe.FxClause) [5]string {
	var values [5]string
	kind := c.Type
	if kind == 3 {
		kind = int32(c.RawType)
	}
	values[0] = strconv.FormatInt(int64(kind), 10)
	values[1] = c.Variable
	values[2] = strconv.FormatInt(int64(c.Comparison), 10)
	switch c.Type {
	case whe.ClauseFloat:
		values[3] = strconv.FormatFloat(float64(c.FloatValue), 'g', -1, 32)
		values[4] = strconv.FormatFloat(float64(c.FloatDefault), 'g', -1, 32)
	case whe.ClauseBoolean:
		values[3] = strconv.FormatBool(c.BoolValue)
		values[4] = strconv.FormatBool(c.BoolDefault)
	case whe.ClauseString:
		values[3] = c.StringValue
		values[4] = c.StringDefault
	default:
		values[0] = strconv.FormatUint(uint64(c.RawType), 10)
	}
	return values
}

func (ui *uiState) layoutClassicOEClauseEditor(gtx layout.Context) layout.Dimensions {
	clause, ok := ui.ensureClassicOEClause()
	if !ok {
		return classicOEHint(gtx, ui.th, "Select a clause above to inspect and edit its definition.")
	}
	labels := [5]string{"Type (0 float / 1 boolean / 2 string)", "Variable", "Comparison (0..5)", "Value", "Default"}
	rows := make([]layout.FlexChild, 0, 7)
	for i := range ui.classicOEClauseEditors {
		i := i
		label := labels[i]
		if i == 3 || i == 4 {
			switch clause.Type {
			case whe.ClauseFloat:
				if i == 3 {
					label = "Value (×100 editor units)"
				} else {
					label = "Default (×100 editor units)"
				}
			case whe.ClauseBoolean:
				label += " (true / false)"
			case whe.ClauseString:
				label += " (string)"
			}
		}
		rows = append(rows, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.classicOEWheEditorRow(gtx, label, &ui.classicOEClauseEditors[i])
		}))
	}
	if ui.classicOEClauseError != "" {
		msg := ui.classicOEClauseError
		rows = append(rows, layout.Rigid(func(gtx layout.Context) layout.Dimensions { return classicOEHint(gtx, ui.th, msg) }))
	}
	rows = append(rows, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return ui.classicOEWheButtons(gtx, &ui.classicOEClauseApply)
	}))
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, rows...)
}

func (ui *uiState) layoutClassicOEClauseList(gtx layout.Context) layout.Dimensions {
	doc, msg := ui.classicOEListDoc()
	if doc == nil {
		return classicOEHint(gtx, ui.th, msg)
	}
	clauses := doc.Clauses()
	if len(clauses) == 0 {
		return classicOEHint(gtx, ui.th, "This .whe defines no clauses.")
	}
	if len(ui.classicOEClauseClicks) != len(clauses) {
		ui.classicOEClauseClicks = make([]widget.Clickable, len(clauses))
	}
	rows := make([]layout.FlexChild, 0, len(clauses)+1)
	for i := range clauses {
		i, clause := i, clauses[i]
		rows = append(rows, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.classicOEClauseRow(gtx, i, clause)
		}))
	}
	rows = append(rows, layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.layoutClassicOEClauseEditor(gtx) }))
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, rows...)
}

func (ui *uiState) classicOEClauseRow(gtx layout.Context, index int, clause whe.FxClause) layout.Dimensions {
	if index < 0 || index >= len(ui.classicOEClauseClicks) {
		return layout.Dimensions{}
	}
	click := &ui.classicOEClauseClicks[index]
	if click.Clicked(gtx) {
		ui.classicOEClauseName = clause.Name
		ui.classicOEClauseBound = ""
		ui.classicOEClauseError = ""
	}
	bg := cPanelBg
	if ui.classicOEClauseName == clause.Name {
		bg = cSelectBg
	} else if click.Hovered() {
		bg = cMenuHover
	}
	return click.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		h := gtx.Dp(unit.Dp(22))
		sz := image.Pt(gtx.Constraints.Max.X, h)
		gtx.Constraints.Min, gtx.Constraints.Max = sz, sz
		return layout.Stack{}.Layout(gtx,
			layout.Expanded(func(gtx layout.Context) layout.Dimensions {
				rrect(gtx, bg, sz, gtx.Dp(2))
				return layout.Dimensions{Size: sz}
			}),
			layout.Stacked(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.th, unit.Sp(10), clause.Name)
							l.Color = cText
							l.MaxLines = 1
							return l.Layout(gtx)
						}),
						layout.Flexed(1, rowGap()),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.th, unit.Sp(9), whe.ClauseTypeName(clause.Type))
							l.Color = cTextDim
							l.MaxLines = 1
							return l.Layout(gtx)
						}),
					)
				})
			}),
		)
	})
}

func (ui *uiState) layoutClassicOEConditionEditorList(gtx layout.Context) layout.Dimensions {
	doc, msg := ui.classicOEListDoc()
	if doc == nil {
		return classicOEHint(gtx, ui.th, msg)
	}
	conditions := doc.Conditions()
	if len(conditions) == 0 {
		return classicOEHint(gtx, ui.th, "This .whe defines no conditions.")
	}
	if len(ui.classicOEConditionClicks) != len(conditions) {
		ui.classicOEConditionClicks = make([]widget.Clickable, len(conditions))
	}
	rows := make([]layout.FlexChild, 0, len(conditions)+1)
	for i := range conditions {
		i, condition := i, conditions[i]
		rows = append(rows, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.classicOEConditionRow(gtx, i, condition)
		}))
	}
	rows = append(rows, layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.layoutClassicOEConditionEditor(gtx) }))
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, rows...)
}

func (ui *uiState) classicOEConditionRow(gtx layout.Context, index int, condition whe.FxCondition) layout.Dimensions {
	if index < 0 || index >= len(ui.classicOEConditionClicks) {
		return layout.Dimensions{}
	}
	click := &ui.classicOEConditionClicks[index]
	if click.Clicked(gtx) {
		ui.classicOEConditionName = condition.Name
		ui.classicOEConditionBound = ""
		ui.classicOEConditionError = ""
	}
	bg := cPanelBg
	if ui.classicOEConditionName == condition.Name {
		bg = cSelectBg
	} else if click.Hovered() {
		bg = cMenuHover
	}
	note := fmt.Sprintf("%d clauses", len(condition.ClauseNames))
	return click.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		h := gtx.Dp(unit.Dp(22))
		sz := image.Pt(gtx.Constraints.Max.X, h)
		gtx.Constraints.Min, gtx.Constraints.Max = sz, sz
		return layout.Stack{}.Layout(gtx,
			layout.Expanded(func(gtx layout.Context) layout.Dimensions {
				rrect(gtx, bg, sz, gtx.Dp(2))
				return layout.Dimensions{Size: sz}
			}),
			layout.Stacked(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.th, unit.Sp(10), condition.Name)
							l.Color = cText
							l.MaxLines = 1
							return l.Layout(gtx)
						}),
						layout.Flexed(1, rowGap()),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.th, unit.Sp(9), note)
							l.Color = cTextDim
							l.MaxLines = 1
							return l.Layout(gtx)
						}),
					)
				})
			}),
		)
	})
}

func (ui *uiState) ensureClassicOECondition() (whe.FxCondition, bool) {
	if ui.unitEvtDoc == nil {
		return whe.FxCondition{}, false
	}
	for _, condition := range ui.unitEvtDoc.Conditions() {
		if strings.EqualFold(condition.Name, ui.classicOEConditionName) {
			bound := ui.unitEvtPath + "|" + condition.Name + "|" + strings.Join(condition.ClauseNames, "\x00")
			if bound != ui.classicOEConditionBound {
				ui.classicOEConditionBound = bound
				ui.classicOEConditionError = ""
				ui.classicOEConditionEditors = make([]widget.Editor, len(condition.ClauseNames))
				ui.classicOEConditionFill = append([]string(nil), condition.ClauseNames...)
				for i, name := range condition.ClauseNames {
					ui.classicOEConditionEditors[i].SetText(name)
				}
			}
			return condition, true
		}
	}
	return whe.FxCondition{}, false
}

func (ui *uiState) layoutClassicOEConditionEditor(gtx layout.Context) layout.Dimensions {
	condition, ok := ui.ensureClassicOECondition()
	if !ok {
		return classicOEHint(gtx, ui.th, "Select a condition to inspect and edit its clause references.")
	}
	rows := make([]layout.FlexChild, 0, len(condition.ClauseNames)+2)
	for i := range ui.classicOEConditionEditors {
		i := i
		rows = append(rows, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.classicOEWheEditorRow(gtx, fmt.Sprintf("Clause reference %d", i+1), &ui.classicOEConditionEditors[i])
		}))
	}
	if ui.classicOEConditionError != "" {
		msg := ui.classicOEConditionError
		rows = append(rows, layout.Rigid(func(gtx layout.Context) layout.Dimensions { return classicOEHint(gtx, ui.th, msg) }))
	}
	rows = append(rows, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.smallButton(gtx, &ui.classicOEConditionAdd, "Add clause")
			}),
			layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.smallButtonDisabled(gtx, &ui.classicOEConditionRemove, "Remove last", len(ui.classicOEConditionEditors) == 0)
			}),
			layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.smallButton(gtx, &ui.classicOEConditionApply, "Apply")
			}),
			layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.smallButton(gtx, &ui.classicOEWheSave, "Save .whe")
			}),
		)
	}))
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, rows...)
}

func (ui *uiState) applyClassicOEClause() {
	if ui.unitEvtDoc == nil {
		ui.classicOEClauseError = "No .whe is loaded."
		return
	}
	var clause whe.FxClause
	found := false
	for _, item := range ui.unitEvtDoc.Clauses() {
		if strings.EqualFold(item.Name, ui.classicOEClauseName) {
			clause, found = item, true
			break
		}
	}
	if !found {
		ui.classicOEClauseError = "Select a clause first."
		return
	}
	kind, err := strconv.ParseInt(strings.TrimSpace(ui.classicOEClauseEditors[0].Text()), 10, 32)
	if err != nil || kind < 0 || kind > 2 {
		ui.classicOEClauseError = "Type must be 0 (float), 1 (boolean), or 2 (string)."
		return
	}
	previousType := clause.Type
	clause.Type = int32(kind)
	clause.RawType = uint32(kind)
	clause.Variable = strings.TrimSpace(ui.classicOEClauseEditors[1].Text())
	comparison, err := strconv.ParseInt(strings.TrimSpace(ui.classicOEClauseEditors[2].Text()), 10, 32)
	if err != nil || comparison < 0 || comparison > 5 {
		ui.classicOEClauseError = "Comparison must be an integer from 0 to 5."
		return
	}
	clause.Comparison = int32(comparison)
	a, b := ui.classicOEClauseEditors[3].Text(), ui.classicOEClauseEditors[4].Text()
	if clause.Type == whe.ClauseFloat || clause.Type == whe.ClauseBoolean {
		a, b = strings.TrimSpace(a), strings.TrimSpace(b)
	}
	switch clause.Type {
	case whe.ClauseFloat:
		if previousType != whe.ClauseFloat {
			a, b = "0", "0"
		}
		value, err1 := strconv.ParseFloat(a, 32)
		def, err2 := strconv.ParseFloat(b, 32)
		if err1 != nil || err2 != nil || math.IsInf(value, 0) || math.IsNaN(value) || math.IsInf(def, 0) || math.IsNaN(def) {
			ui.classicOEClauseError = "Value and default must be finite numbers."
			return
		}
		clause.FloatValue, clause.FloatDefault = float32(value), float32(def)
		clause.BoolValue, clause.BoolDefault = false, false
		clause.StringValue, clause.StringDefault = "", ""
	case whe.ClauseBoolean:
		if previousType != whe.ClauseBoolean {
			a, b = "false", "false"
		}
		value, err1 := strconv.ParseBool(a)
		def, err2 := strconv.ParseBool(b)
		if err1 != nil || err2 != nil {
			ui.classicOEClauseError = "Boolean values must be true or false."
			return
		}
		clause.BoolValue, clause.BoolDefault = value, def
		clause.FloatValue, clause.FloatDefault = 0, 0
		clause.StringValue, clause.StringDefault = "", ""
	case whe.ClauseString:
		if previousType != whe.ClauseString {
			a, b = "", ""
		}
		clause.StringValue, clause.StringDefault = a, b
		clause.FloatValue, clause.FloatDefault = 0, 0
		clause.BoolValue, clause.BoolDefault = false, false
	}
	if err := ui.unitEvtDoc.SetClause(clause.Name, clause); err != nil {
		ui.classicOEClauseError = err.Error()
		return
	}
	ui.unitEvtDirty = true
	ui.classicOEClauseBound = ""
	ui.classicOEClauseError = ""
	ui.statusText = "Applied clause " + clause.Name + " — Save .whe to write it into the mod"
}

func (ui *uiState) applyClassicOECondition() {
	if ui.unitEvtDoc == nil {
		ui.classicOEConditionError = "No .whe is loaded."
		return
	}
	condition, ok := ui.ensureClassicOECondition()
	if !ok {
		ui.classicOEConditionError = "Select a condition first."
		return
	}
	refs := make([]string, len(ui.classicOEConditionEditors))
	for i := range refs {
		refs[i] = strings.TrimSpace(ui.classicOEConditionEditors[i].Text())
		if refs[i] == "" {
			ui.classicOEConditionError = fmt.Sprintf("Clause reference %d cannot be empty.", i+1)
			return
		}
	}
	condition.ClauseNames = refs
	if err := ui.unitEvtDoc.SetCondition(condition.Name, condition); err != nil {
		ui.classicOEConditionError = err.Error()
		return
	}
	ui.unitEvtDirty = true
	ui.classicOEConditionBound = ""
	ui.classicOEConditionError = ""
	ui.statusText = "Applied condition " + condition.Name + " — Save .whe to write it into the mod"
}

func (ui *uiState) handleClassicOEClauseConditionInput(gtx layout.Context) {
	if ui.currentUnitSection().label != unitSectionClassicOE || ui.unitEvtDoc == nil {
		return
	}
	for i := range ui.classicOEClauseEditors {
		ui.updateClassicOEWheEditor(gtx, &ui.classicOEClauseEditors[i], ui.applyClassicOEClause)
	}
	for i := range ui.classicOEConditionEditors {
		ui.updateClassicOEWheEditor(gtx, &ui.classicOEConditionEditors[i], ui.applyClassicOECondition)
	}
	if ui.classicOEClauseApply.Clicked(gtx) {
		ui.applyClassicOEClause()
	}
	if ui.classicOEConditionApply.Clicked(gtx) {
		ui.applyClassicOECondition()
	}
	if ui.classicOEConditionAdd.Clicked(gtx) {
		if _, ok := ui.ensureClassicOECondition(); ok {
			ui.classicOEConditionEditors = append(ui.classicOEConditionEditors, widget.Editor{SingleLine: true, Submit: true})
			ui.classicOEConditionFill = append(ui.classicOEConditionFill, "")
		}
	}
	if ui.classicOEConditionRemove.Clicked(gtx) && len(ui.classicOEConditionEditors) > 0 {
		ui.classicOEConditionEditors = ui.classicOEConditionEditors[:len(ui.classicOEConditionEditors)-1]
		ui.classicOEConditionFill = ui.classicOEConditionFill[:len(ui.classicOEConditionFill)-1]
	}
}

// ─── merged from unit_classic_oe_whe_ui.go ───────────────────────────────────────────────

var classicOESelectedUIFields = [20]string{
	"Display type (circle / square)",
	"Indicator scale X", "Indicator scale Z",
	"Indicator offset X", "Indicator offset Z",
	"Volume offset X", "Volume offset Y", "Volume offset Z",
	"Volume scale X", "Volume scale Y", "Volume scale Z",
	"Matrix 1,1", "Matrix 1,2", "Matrix 1,3",
	"Matrix 2,1", "Matrix 2,2", "Matrix 2,3",
	"Matrix 3,1", "Matrix 3,2", "Matrix 3,3",
}

var classicOESelectedUIMatrixFields = [9]string{
	"Matrix 1,1", "Matrix 1,2", "Matrix 1,3",
	"Matrix 2,1", "Matrix 2,2", "Matrix 2,3",
	"Matrix 3,1", "Matrix 3,2", "Matrix 3,3",
}

func selectedUIValues(v whe.FxSelectedUI) [20]string {
	var out [20]string
	out[0] = strconv.FormatInt(int64(v.DisplayType), 10)
	at := 1
	for _, pair := range [][2]float32{v.Scale, v.Offset} {
		for _, n := range pair {
			out[at] = strconv.FormatFloat(float64(n), 'g', -1, 32)
			at++
		}
	}
	for _, triple := range [][3]float32{v.VolumeOffset, v.VolumeScale} {
		for _, n := range triple {
			out[at] = strconv.FormatFloat(float64(n), 'g', -1, 32)
			at++
		}
	}
	for _, row := range v.Matrix {
		for _, n := range row {
			out[at] = strconv.FormatFloat(float64(n), 'g', -1, 32)
			at++
		}
	}
	return out
}

func selectedUIDisplayTypeCode(shape string) (int32, error) {
	switch shape {
	case "circle":
		return 0, nil
	case "square":
		return 1, nil
	default:
		return 0, fmt.Errorf("display type must be circle or square")
	}
}

func selectedUIFromValues(v whe.FxSelectedUI, values [20]string) (whe.FxSelectedUI, error) {
	kind, err := strconv.ParseInt(strings.TrimSpace(values[0]), 10, 32)
	if err != nil {
		return v, fmt.Errorf("display type must be circle or square")
	}
	if kind != 0 && kind != 1 {
		return v, fmt.Errorf("display type must be 0 (circle) or 1 (square)")
	}
	v.DisplayType = int32(kind)
	parse := func(index int) (float32, error) {
		n, err := strconv.ParseFloat(strings.TrimSpace(values[index]), 32)
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
			return 0, fmt.Errorf("%s: enter a finite number", classicOESelectedUIFields[index])
		}
		return float32(n), nil
	}
	at := 1
	for pairIndex := range [][2]float32{v.Scale, v.Offset} {
		pair := [2]float32{}
		for i := range pair {
			pair[i], err = parse(at)
			if err != nil {
				return v, err
			}
			at++
		}
		if pairIndex == 0 {
			v.Scale = pair
		} else {
			v.Offset = pair
		}
	}
	for tripleIndex := range [][3]float32{v.VolumeOffset, v.VolumeScale} {
		triple := [3]float32{}
		for i := range triple {
			triple[i], err = parse(at)
			if err != nil {
				return v, err
			}
			at++
		}
		if tripleIndex == 0 {
			v.VolumeOffset = triple
		} else {
			v.VolumeScale = triple
		}
	}
	for row := range v.Matrix {
		for col := range v.Matrix[row] {
			v.Matrix[row][col], err = parse(at)
			if err != nil {
				return v, err
			}
			at++
		}
	}
	return v, nil
}

func (ui *uiState) ensureClassicOESelectedUI() (whe.FxSelectedUI, bool, error) {
	ui.syncUnitSelectionIndicator(ui.unitModelViewer())
	if ui.unitEvtDoc == nil {
		return whe.FxSelectedUI{}, false, fmt.Errorf("%s", ui.unitEvtErr)
	}
	v, present, err := ui.unitEvtDoc.SelectedUI()
	if err != nil {
		return v, present, err
	}
	if !present {
		return v, false, nil
	}
	values := selectedUIValues(v)
	if ui.classicOESelectedUIType == nil {
		ui.classicOESelectedUIType = &widget.Enum{}
	}
	bound := ui.unitEvtPath
	for i := range values {
		bound += "|" + values[i]
	}
	if bound != ui.classicOESelectedUIBound {
		ui.classicOESelectedUIBound = bound
		ui.classicOESelectedUIError = ""
		for i := range values {
			ui.classicOESelectedUIEditors[i].SetText(values[i])
			ui.classicOESelectedUIFill[i] = values[i]
		}
		if displayType, err := selectedUIDisplayTypeCodeFromValue(v.DisplayType); err == nil {
			ui.classicOESelectedUIType.Value = displayType
		} else {
			ui.classicOESelectedUIType.Value = "circle"
		}
	}
	if model := ui.unitModelViewer(); model != nil {
		indicator := &engine.SelectionIndicator{DisplayType: v.DisplayType, Scale: v.Scale, Offset: v.Offset}
		if model.selectionIndicator == nil || *model.selectionIndicator != *indicator {
			model.selectionIndicator = indicator
			model.invalidate()
		}
	}
	return v, true, nil
}

func (ui *uiState) layoutClassicOESelectedUI(gtx layout.Context) layout.Dimensions {
	_, present, err := ui.ensureClassicOESelectedUI()
	if err != nil {
		return classicOEHint(gtx, ui.th, "Could not read DATASEUI: "+err.Error())
	}
	if !present {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return classicOEHint(gtx, ui.th, "This .whe has no DATASEUI chunk.")
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.smallButton(gtx, &ui.classicOESelectedUICreate, "Create Selected UI")
			}),
		)
	}
	var children []layout.FlexChild
	children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return ui.layoutClassicOESelectedUIVisibility(gtx)
	}))
	children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return ui.layoutClassicOESelectedUIDisplayType(gtx)
	}))
	children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return ui.layoutClassicOESelectedUIPair(gtx, "Indicator scale", "X", "Z", 1)
	}))
	children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return ui.layoutClassicOESelectedUIPair(gtx, "Indicator offset", "X", "Z", 3)
	}))
	children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return ui.layoutClassicOESelectedUITriple(gtx, "Volume offset", "X", "Y", "Z", 5)
	}))
	children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return ui.layoutClassicOESelectedUITriple(gtx, "Volume scale", "X", "Y", "Z", 8)
	}))
	children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return ui.layoutClassicOESelectedUIMatrix(gtx)
	}))
	if ui.classicOESelectedUIError != "" {
		msg := ui.classicOESelectedUIError
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions { return classicOEHint(gtx, ui.th, msg) }))
	}
	children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return ui.classicOEWheButtons(gtx, &ui.classicOESelectedUIApply)
	}))
	return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	})
}

func selectedUIDisplayTypeCodeFromValue(displayType int32) (string, error) {
	switch displayType {
	case 0:
		return "circle", nil
	case 1:
		return "square", nil
	default:
		return "", fmt.Errorf("unknown display type %d", displayType)
	}
}

func (ui *uiState) layoutClassicOESelectedUIVisibility(gtx layout.Context) layout.Dimensions {
	v := ui.unitModelViewer()
	if v == nil {
		return layout.Dimensions{}
	}
	if ui.classicOESelectedUIVisible == nil {
		ui.classicOESelectedUIVisible = &widget.Bool{Value: v.showSelection}
	} else if ui.classicOESelectedUIVisible.Value != v.showSelection {
		ui.classicOESelectedUIVisible.Value = v.showSelection
	}
	return layout.Inset{Top: unit.Dp(2), Bottom: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		check := material.CheckBox(ui.th, ui.classicOESelectedUIVisible, "Show selection outline")
		check.TextSize = unit.Sp(9)
		return check.Layout(gtx)
	})
}

func (ui *uiState) layoutClassicOESelectedUIDisplayType(gtx layout.Context) layout.Dimensions {
	if ui.classicOESelectedUIType == nil {
		ui.classicOESelectedUIType = &widget.Enum{}
	}
	return layout.Inset{Top: unit.Dp(2), Bottom: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				label := material.Label(ui.th, unit.Sp(9), "Display type")
				label.Color = cTextDim
				return label.Layout(gtx)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						radio := material.RadioButton(ui.th, ui.classicOESelectedUIType, "circle", "Circle")
						radio.TextSize = unit.Sp(9)
						return radio.Layout(gtx)
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						radio := material.RadioButton(ui.th, ui.classicOESelectedUIType, "square", "Square")
						radio.TextSize = unit.Sp(9)
						return radio.Layout(gtx)
					}),
				)
			}),
		)
	})
}

func (ui *uiState) layoutClassicOESelectedUIPair(gtx layout.Context, title, axis1, axis2 string, first int) layout.Dimensions {
	return ui.layoutClassicOESelectedUIAxes(gtx, title, []string{axis1, axis2}, first)
}

func (ui *uiState) layoutClassicOESelectedUITriple(gtx layout.Context, title, axis1, axis2, axis3 string, first int) layout.Dimensions {
	return ui.layoutClassicOESelectedUIAxes(gtx, title, []string{axis1, axis2, axis3}, first)
}

func (ui *uiState) layoutClassicOESelectedUIAxes(gtx layout.Context, title string, axes []string, first int) layout.Dimensions {
	return layout.Inset{Top: unit.Dp(2), Bottom: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				label := material.Label(ui.th, unit.Sp(9), title)
				label.Color = cTextDim
				return label.Layout(gtx)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx, func() []layout.FlexChild {
					children := make([]layout.FlexChild, 0, len(axes)*3-1)
					for i, axis := range axes {
						i, axis := i, axis
						if i != 0 {
							children = append(children, layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout))
						}
						children = append(children,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								label := material.Label(ui.th, unit.Sp(8), axis)
								label.Color = cTextDim
								gtx.Constraints.Min.X = gtx.Dp(12)
								gtx.Constraints.Max.X = gtx.Dp(12)
								return layout.Center.Layout(gtx, label.Layout)
							}),
							layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
								return ui.classicOEWheEditorField(gtx, &ui.classicOESelectedUIEditors[first+i])
							}),
						)
					}
					return children
				}()...)
			}),
		)
	})
}

func (ui *uiState) layoutClassicOESelectedUIMatrix(gtx layout.Context) layout.Dimensions {
	return layout.Inset{Top: unit.Dp(2), Bottom: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				label := material.Label(ui.th, unit.Sp(9), "Matrix")
				label.Color = cTextDim
				return label.Layout(gtx)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(layout.Spacer{Width: unit.Dp(22)}.Layout),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return ui.layoutClassicOESelectedUIAxisHeadings(gtx, "X", "Y", "Z")
					}),
				)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					ui.layoutClassicOESelectedUIMatrixRow(gtx, 0, "X"),
					ui.layoutClassicOESelectedUIMatrixRow(gtx, 1, "Y"),
					ui.layoutClassicOESelectedUIMatrixRow(gtx, 2, "Z"),
				)
			}),
		)
	})
}

func (ui *uiState) layoutClassicOESelectedUIAxisHeadings(gtx layout.Context, axes ...string) layout.Dimensions {
	children := make([]layout.FlexChild, 0, len(axes)*2-1)
	for i, axis := range axes {
		axis := axis
		children = append(children, layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			label := material.Label(ui.th, unit.Sp(8), axis)
			label.Color = cTextDim
			return layout.Center.Layout(gtx, label.Layout)
		}))
		if i != len(axes)-1 {
			children = append(children, layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout))
		}
	}
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx, children...)
}

func (ui *uiState) layoutClassicOESelectedUIMatrixRow(gtx layout.Context, row int, axis string) layout.FlexChild {
	return layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(1), Bottom: unit.Dp(1)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Min.Y, gtx.Constraints.Max.Y = gtx.Dp(22), gtx.Dp(22)
					return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						label := material.Label(ui.th, unit.Sp(8), axis)
						label.Color = cTextDim
						return label.Layout(gtx)
					})
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal}.Layout(gtx, func() []layout.FlexChild {
						children := make([]layout.FlexChild, 0, 5)
						for col := 0; col < 3; col++ {
							index := 11 + row*3 + col
							children = append(children, layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
								return ui.classicOEWheEditorField(gtx, &ui.classicOESelectedUIEditors[index])
							}))
							if col != 2 {
								children = append(children, layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout))
							}
						}
						return children
					}()...)
				}),
			)
		})
	})
}

func (ui *uiState) classicOEWheEditorField(gtx layout.Context, ed *widget.Editor) layout.Dimensions {
	ed.SingleLine, ed.Submit = true, true
	gtx.Constraints.Min.Y, gtx.Constraints.Max.Y = gtx.Dp(22), gtx.Dp(22)
	return widget.Border{Color: cBorderLight, Width: unit.Dp(1), CornerRadius: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Left: unit.Dp(4), Right: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			w := material.Editor(ui.th, ed, "")
			w.TextSize = unit.Sp(10)
			return w.Layout(gtx)
		})
	})
}

func (ui *uiState) ensureClassicOEModifier() (whe.FxModifier, bool) {
	if ui.unitEvtDoc == nil {
		return whe.FxModifier{}, false
	}
	mods := ui.unitEvtDoc.Modifiers()
	for _, m := range mods {
		if strings.EqualFold(m.Name, ui.classicOEModifierName) {
			bound := ui.unitEvtPath + "|" + m.Name + "|" + m.Variable + "|" + strconv.Itoa(int(m.Type)) + "|" + strconv.FormatFloat(float64(m.Reference), 'g', -1, 32) + "|" + strconv.FormatFloat(float64(m.Default), 'g', -1, 32)
			if bound != ui.classicOEModifierBound {
				ui.classicOEModifierBound = bound
				ui.classicOEModifierError = ""
				texts := [4]string{m.Variable, strconv.FormatInt(int64(m.Type), 10), strconv.FormatFloat(float64(m.Reference), 'g', -1, 32), strconv.FormatFloat(float64(m.Default), 'g', -1, 32)}
				for i := range texts {
					ui.classicOEModifierEditors[i].SetText(texts[i])
					ui.classicOEModifierFill[i] = texts[i]
				}
			}
			return m, true
		}
	}
	return whe.FxModifier{}, false
}

func (ui *uiState) layoutClassicOEModifierEditor(gtx layout.Context) layout.Dimensions {
	m, ok := ui.ensureClassicOEModifier()
	if !ok {
		return classicOEHint(gtx, ui.th, "Select a modifier above to inspect and edit its definition.")
	}
	children := []layout.FlexChild{}
	labels := [4]string{"Variable", "Type (0 absolute time / 1 speed scale)", "Reference value", "Default value"}
	for i := range ui.classicOEModifierEditors {
		i := i
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.classicOEWheEditorRow(gtx, labels[i], &ui.classicOEModifierEditors[i])
		}))
	}
	_ = m
	if ui.classicOEModifierError != "" {
		msg := ui.classicOEModifierError
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions { return classicOEHint(gtx, ui.th, msg) }))
	}
	children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return ui.classicOEWheButtons(gtx, &ui.classicOEModifierApply)
	}))
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}

func (ui *uiState) classicOEWheEditorRow(gtx layout.Context, label string, ed *widget.Editor) layout.Dimensions {
	return layout.Inset{Top: unit.Dp(2), Bottom: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				l := material.Label(ui.th, unit.Sp(9), label)
				l.Color = cTextDim
				return l.Layout(gtx)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.classicOEWheEditorField(gtx, ed)
			}),
		)
	})
}

func (ui *uiState) classicOEWheButtons(gtx layout.Context, apply *widget.Clickable) layout.Dimensions {
	return layout.Inset{Top: unit.Dp(5)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.smallButton(gtx, apply, "Apply") }),
			layout.Rigid(layout.Spacer{Width: unit.Dp(5)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.smallButton(gtx, &ui.classicOEWheSave, "Save .whe")
			}),
			layout.Rigid(layout.Spacer{Width: unit.Dp(5)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.smallButtonDisabled(gtx, &ui.classicOEWheRevert, "Revert", !ui.unitEvtDirty)
			}),
		)
	})
}

func (ui *uiState) handleClassicOEWheInput(gtx layout.Context) {
	if ui.currentUnitSection().label != unitSectionClassicOE {
		return
	}
	if ui.unitEvtDoc == nil {
		return
	}
	for i := range ui.classicOESelectedUIEditors {
		if i == 0 {
			continue // Display type is selected with the Circle/Square radio buttons.
		}
		ui.updateClassicOEWheEditor(gtx, &ui.classicOESelectedUIEditors[i], func() { ui.applyClassicOESelectedUI() })
	}
	for i := range ui.classicOEModifierEditors {
		ui.updateClassicOEWheEditor(gtx, &ui.classicOEModifierEditors[i], func() { ui.applyClassicOEModifier() })
	}
	if ui.classicOESelectedUICreate.Clicked(gtx) {
		ui.createClassicOESelectedUI()
	}
	if ui.classicOESelectedUIVisible != nil && ui.classicOESelectedUIVisible.Update(gtx) {
		if v := ui.unitModelViewer(); v != nil {
			v.showSelection = ui.classicOESelectedUIVisible.Value
			v.invalidate()
		}
	}
	if ui.classicOESelectedUIApply.Clicked(gtx) {
		ui.applyClassicOESelectedUI()
	}
	if ui.classicOEModifierApply.Clicked(gtx) {
		ui.applyClassicOEModifier()
	}
	if ui.classicOEWheSave.Clicked(gtx) {
		ui.saveClassicOEWhe()
	}
	if ui.classicOEWheRevert.Clicked(gtx) {
		ui.revertUnitEvents()
		ui.classicOESelectedUIBound = ""
		ui.classicOEModifierBound = ""
	}
}

func (ui *uiState) updateClassicOEWheEditor(gtx layout.Context, ed *widget.Editor, submit func()) {
	if ui.rgdEnsureEditorClick(ed).click.Clicked(gtx) {
		gtx.Execute(key.FocusCmd{Tag: ed})
	}
	for {
		ev, ok := ed.Update(gtx)
		if !ok {
			break
		}
		if _, ok := ev.(widget.SubmitEvent); ok {
			submit()
			ed.Blur(gtx)
		}
	}
}

func (ui *uiState) createClassicOESelectedUI() {
	if ui.unitEvtDoc == nil {
		ui.classicOESelectedUIError = "No .whe is loaded."
		return
	}
	value := whe.FxSelectedUI{DisplayType: 0}
	value.Scale = [2]float32{1, 1}
	value.VolumeScale = [3]float32{1, 1, 1}
	for i := range value.Matrix {
		value.Matrix[i][i] = 1
	}
	if err := ui.unitEvtDoc.SetSelectedUI(value); err != nil {
		ui.classicOESelectedUIError = err.Error()
		return
	}
	ui.unitEvtDirty = true
	ui.classicOESelectedUIBound = ""
	ui.classicOESelectedUIError = ""
	ui.updateUnitSelectionIndicator(value)
	ui.statusText = "Created DATASEUI — Save .whe to write it into the mod"
}

func (ui *uiState) updateUnitSelectionIndicator(value whe.FxSelectedUI) {
	if v := ui.unitModelViewer(); v != nil {
		indicator := &engine.SelectionIndicator{DisplayType: value.DisplayType, Scale: value.Scale, Offset: value.Offset}
		if v.selectionIndicator == nil || *v.selectionIndicator != *indicator {
			v.selectionIndicator = indicator
			v.invalidate()
		}
	}
}

// syncUnitSelectionIndicator loads the selected unit's DATASEUI values for
// either model panel and clears the previous unit's indicator when none exists.
func (ui *uiState) syncUnitSelectionIndicator(v *modelViewer) {
	if v == nil {
		return
	}
	ui.ensureUnitEvents()
	var indicator *engine.SelectionIndicator
	if ui.unitEvtDoc != nil {
		value, present, err := ui.unitEvtDoc.SelectedUI()
		if err == nil && present {
			indicator = &engine.SelectionIndicator{DisplayType: value.DisplayType, Scale: value.Scale, Offset: value.Offset}
		}
	}
	if v.selectionIndicator == nil && indicator == nil {
		return
	}
	if v.selectionIndicator != nil && indicator != nil && *v.selectionIndicator == *indicator {
		return
	}
	v.selectionIndicator = indicator
	v.invalidate()
}

func (ui *uiState) applyClassicOESelectedUI() {
	if ui.unitEvtDoc == nil {
		ui.classicOESelectedUIError = "No .whe is loaded."
		return
	}
	value, present, err := ui.unitEvtDoc.SelectedUI()
	if err != nil {
		ui.classicOESelectedUIError = err.Error()
		return
	}
	if !present {
		ui.classicOESelectedUIError = "This .whe has no DATASEUI chunk."
		return
	}
	var values [20]string
	for i := range values {
		values[i] = ui.classicOESelectedUIEditors[i].Text()
	}
	if ui.classicOESelectedUIType != nil {
		displayType, err := selectedUIDisplayTypeCode(ui.classicOESelectedUIType.Value)
		if err != nil {
			ui.classicOESelectedUIError = err.Error()
			return
		}
		values[0] = strconv.FormatInt(int64(displayType), 10)
	}
	value, err = selectedUIFromValues(value, values)
	if err != nil {
		ui.classicOESelectedUIError = err.Error()
		return
	}
	if err = ui.unitEvtDoc.SetSelectedUI(value); err != nil {
		ui.classicOESelectedUIError = err.Error()
		return
	}
	ui.unitEvtDirty = true
	ui.classicOESelectedUIBound = ""
	ui.classicOESelectedUIError = ""
	ui.updateUnitSelectionIndicator(value)
	ui.statusText = "Applied selection volume settings — Save .whe to write them into the mod"
}

func (ui *uiState) applyClassicOEModifier() {
	if ui.unitEvtDoc == nil {
		ui.classicOEModifierError = "No .whe is loaded."
		return
	}
	var value whe.FxModifier
	for _, m := range ui.unitEvtDoc.Modifiers() {
		if strings.EqualFold(m.Name, ui.classicOEModifierName) {
			value = m
			break
		}
	}
	if value.Name == "" {
		ui.classicOEModifierError = "Select a modifier first."
		return
	}
	value.Variable = strings.TrimSpace(ui.classicOEModifierEditors[0].Text())
	kind, err := strconv.ParseInt(strings.TrimSpace(ui.classicOEModifierEditors[1].Text()), 10, 32)
	if err != nil || kind < 0 || kind > 1 {
		ui.classicOEModifierError = "Type must be 0 (absolute time) or 1 (speed scale)."
		return
	}
	value.Type = int32(kind)
	for i, dst := range []*float32{&value.Reference, &value.Default} {
		n, parseErr := strconv.ParseFloat(strings.TrimSpace(ui.classicOEModifierEditors[i+2].Text()), 32)
		if parseErr != nil {
			ui.classicOEModifierError = "Reference and default values must be finite numbers."
			return
		}
		*dst = float32(n)
	}
	if err = ui.unitEvtDoc.SetModifier(value.Name, value); err != nil {
		ui.classicOEModifierError = err.Error()
		return
	}
	ui.unitEvtDirty = true
	ui.classicOEModifierBound = ""
	ui.classicOEModifierError = ""
	ui.statusText = "Applied modifier " + value.Name + " — Save .whe to write it into the mod"
}

func (ui *uiState) saveClassicOEWhe() {
	line, err := ui.saveUnitEvents()
	if err != nil {
		ui.statusText = "Save failed: " + err.Error()
		return
	}
	ui.statusText = line
	ui.markStateDirty()
}
