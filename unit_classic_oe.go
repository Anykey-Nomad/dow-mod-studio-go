package main

import (
	"dow-mod-studio/internal/whe"
	"fmt"
	"image"
	"sort"
	"strings"

	"gioui.org/layout"
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
	panelW := gtx.Dp(unit.Dp(classicOEPanelDp))
	gap := gtx.Dp(unit.Dp(unitColGapDp))
	return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
		layout.Rigid(ui.fixedPanel(panelW, ui.layoutClassicOELeftPanel)),
		layout.Rigid(layout.Spacer{Width: unit.Dp(gap)}.Layout),
		layout.Flexed(1, ui.layoutClassicOEPreview),
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
		return ui.layoutClassicOEAnimList(gtx)
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
