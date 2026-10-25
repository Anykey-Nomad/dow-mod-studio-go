package main

import (
	"image"
	"image/color"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

// dimOverlayColor is the scrim behind a modal window.
var dimOverlayColor = color.NRGBA{R: 0, G: 0, B: 0, A: 90}

// ── unit / building interface ──────────────────────────────────────────────
//
// This is the skeleton of the object interface: a tool bar that opens it, and
// the three column window itself — a narrow section list, a middle list and a
// wide detail area. Nothing is wired to data yet on purpose, so the shape of
// the window can be agreed on before any of it starts reading a file.

// unitSection describes one entry of the dialog's left menu, together with the
// headings its middle and right columns carry. The headings are what make the
// four views tellable apart before they have any content.
type unitSection struct {
	label       string
	middleTitle string
	rightTitle  string
	// hint is the placeholder shown in the middle column, stating what will
	// live there once the section is filled in.
	hint string
}

var unitSections = []unitSection{
	{
		label:       "Unit info",
		middleTitle: "Info",
		rightTitle:  "Properties",
		hint:        "Unit and building properties will be listed here.",
	},
	{
		label:       "Weapons",
		middleTitle: "Weapons",
		rightTitle:  "Weapon properties",
		hint:        "The unit's weapons will be listed here.",
	},
	{
		label:       "Abilities",
		middleTitle: "Abilities",
		rightTitle:  "Ability properties",
		hint:        "The unit's abilities will be listed here.",
	},
	{
		label:       "Squads",
		middleTitle: "Squads",
		rightTitle:  "Squad properties",
		hint:        "The squads this unit belongs to will be listed here.",
	},
	{
		label:       "Links",
		middleTitle: "Links",
		rightTitle:  "Link",
		hint:        "What this unit refers to, and what refers to it, will be listed here.",
	},
	{
		label:       "Frozen",
		middleTitle: "Frozen units and buildings",
		rightTitle:  "Freeze",
		hint:        "The units and buildings that have been moved out of the mod, and what was cleared to move them.",
	},
	{
		label:       "Problems",
		middleTitle: "Broken references",
		rightTitle:  "Reference",
		hint:        "References anywhere in the mod that name a file which is not there.",
	},
	{
		label:       "Events",
		middleTitle: "Timelines",
		rightTitle:  "Events",
		hint:        "The model's .whe timelines, with the FX and sounds they fire.",
	},
	{
		label:       "Viewport",
		middleTitle: "Viewport",
		rightTitle:  "Model",
		hint:        "The unit's model, with the animations it can be played with.",
	},
	{
		label:       unitSectionClassicOE,
		middleTitle: "Classic OE",
		rightTitle:  "Properties",
		hint:        "The classic object editor's layout: its panel column, with the unit previewed beside it.",
	},
}

// unitSectionNamed is where a section of the dialog is, by name. It is here
// because a section is referred to by name in more than one place — the freeze
// operation moves the window to the Frozen section when it is done, for one —
// and counting the entries in a slice to get there is how that goes wrong the
// next time a section is added.
func unitSectionNamed(label string) int {
	for i, s := range unitSections {
		if s.label == label {
			return i
		}
	}
	return 0
}

// Geometry of the columns. The left column is sized to read as a menu rather
// than a list, the middle takes a third of the space, and the right column
// takes whatever is left.
const (
	unitLeftColumnDp    = 132
	unitRowHeightDp     = 26
	unitColHeaderHeight = 26
	unitColGapDp        = 8
	unitColMinWidthDp   = 140
)

// layoutUnitPickerRow places the race and unit strip above the three columns.
// The strip's height is fixed and the dropdowns are drawn over the columns, so
// opening one does not move the columns the user is reading.
func (ui *uiState) layoutUnitPickerRow(gtx layout.Context) layout.Dimensions {
	h := gtx.Dp(unit.Dp(unitPickerStripHeightDp))
	gtx.Constraints.Min.Y = h
	gtx.Constraints.Max.Y = h
	return ui.layoutUnitPickerStrip(gtx)
}

// unitObjectLabel is what the dialog calls the object it was opened for. It
// names the unit the pickers have selected, because that is what the columns
// below show; the unit's own display name is preferred, and its file name is
// the fallback, since that is the name the game itself uses.
func (ui *uiState) unitObjectLabel() string {
	if d := ui.currentUnitDetails(); d != nil {
		if d.name != "" {
			return d.name
		}
		if d.entry.name != "" {
			return d.entry.name
		}
	}
	if u := ui.currentUnit(); u != nil {
		return u.displayName()
	}
	return "No unit selected"
}

// currentUnitSection is the section the dialog is showing, guarded so an index
// left over from a future section list cannot panic the layout.
func (ui *uiState) currentUnitSection() unitSection {
	if ui.unitSection < 0 || ui.unitSection >= len(unitSections) {
		return unitSections[0]
	}
	return unitSections[ui.unitSection]
}

// ── tool bar panel ─────────────────────────────────────────────────────────

// unitToolButtons are the buttons of the small panel that sits in the menu bar
// to the left of the search field. Only the object interface exists so far; the
// list is what the panel iterates over, so another button is one line here.
var unitToolButtons = []struct {
	label  string
	action string // handled in handleUnitToolInput
}{
	{label: "Object", action: "unit-interface"},
}

// layoutUnitPanelSlot places the tool panel left of the search field, with the
// same margins so both sit on one baseline in the menu bar.
func (ui *uiState) layoutUnitPanelSlot(gtx layout.Context) layout.Dimensions {
	return layout.Inset{Right: unitPanelGapDp, Top: unit.Dp(1), Bottom: unit.Dp(1)}.Layout(gtx, ui.layoutUnitPanel)
}

const unitPanelGapDp = 10

// layoutUnitPanel draws the frame around the tool buttons. It measures its
// content first: the panel is a Rigid in the menu bar's flex row, so its
// constraints still hold all the free width of the bar, and a background sized
// from them would stretch from the buttons to the right edge.
func (ui *uiState) layoutUnitPanel(gtx layout.Context) layout.Dimensions {
	ui.ensureUnitClicks()
	buttons := func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(2), Bottom: unit.Dp(2), Left: unit.Dp(2), Right: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			var children []layout.FlexChild
			for i := range unitToolButtons {
				i := i
				children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Right: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return ui.unitToolButton(gtx, &ui.unitToolClicks[i], unitToolButtons[i].label, ui.unitTabActive())
					})
				}))
			}
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx, children...)
		})
	}
	return frameMeasured(gtx, buttons, cSaveBg, cSaveBd, unit.Dp(3))
}

// unitToolButton is one button of the tool panel: a label with its own frame,
// measured the same way because it too sits in a flex row.
func (ui *uiState) unitToolButton(gtx layout.Context, btn *widget.Clickable, label string, active bool) layout.Dimensions {
	bg := cSaveBg
	col := cSaveBd
	if active {
		bg, col = cSelectBg, cSelectBd
	} else if btn.Hovered() {
		bg, col = cSaveHover, cSelectBd
	}
	textCol := cText
	if active {
		textCol = rgb(0xFFFFFF)
	}
	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return frameMeasured(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Left: unit.Dp(10), Right: unit.Dp(10), Top: unit.Dp(3), Bottom: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				l := material.Label(ui.th, unit.Sp(11), label)
				l.Color = textCol
				l.MaxLines = 1
				return l.Layout(gtx)
			})
		}, bg, col, unit.Dp(3))
	})
}

// frameMeasured draws content inside a rounded, bordered frame that hugs it.
// The content is laid out into a recorded op stream first to learn its size,
// the frame is painted at that size, and the recording is replayed on top.
func frameMeasured(gtx layout.Context, content layout.Widget, bg, border color.NRGBA, radius unit.Dp) layout.Dimensions {
	macro := op.Record(gtx.Ops)
	dims := content(gtx)
	call := macro.Stop()
	sz := dims.Size
	rrect(gtx, bg, sz, gtx.Dp(radius))
	frame := gtx
	frame.Constraints.Min = sz
	frame.Constraints.Max = sz
	widget.Border{Color: border, Width: unit.Dp(1), CornerRadius: radius}.Layout(frame, func(gtx layout.Context) layout.Dimensions {
		return layout.Dimensions{Size: gtx.Constraints.Min}
	})
	clip.Rect{Max: sz}.Push(gtx.Ops).Pop()
	call.Add(gtx.Ops)
	return layout.Dimensions{Size: sz}
}

func (ui *uiState) ensureUnitClicks() {
	if len(ui.unitSectionClicks) != len(unitSections) {
		ui.unitSectionClicks = make([]widget.Clickable, len(unitSections))
	}
	if len(ui.unitToolClicks) != len(unitToolButtons) {
		ui.unitToolClicks = make([]widget.Clickable, len(unitToolButtons))
	}
}

// ── input ──────────────────────────────────────────────────────────────────

// handleUnitToolInput opens the object interface from the menu bar. It runs on
// every frame, so a click here must not be swallowed by another branch. The
// interface is a tab now, so the button selects or opens that tab rather than
// raising a window.
func (ui *uiState) handleUnitToolInput(gtx layout.Context) {
	ui.ensureUnitClicks()
	for i := range ui.unitToolClicks {
		if !ui.unitToolClicks[i].Clicked(gtx) {
			continue
		}
		switch unitToolButtons[i].action {
		case "unit-interface":
			alreadyOpen := ui.unitTabActive()
			ui.openUnitTab()
			ui.unitSection = 0
			ui.markStateDirty()
			if alreadyOpen {
				// Pressing the button while the tab is on screen would
				// otherwise leave it looking like nothing happened.
				ui.statusText = "Object interface is already open"
			} else {
				ui.statusText = "Object interface: " + ui.unitObjectLabel()
			}
		}
		return
	}
}

// handleUnitTabInput owns the object tab's own clicks: the pickers, the
// selectable rows of the populated columns, and the section list. It runs as
// part of the ordinary input path, so each part consumes only its own clicks.
func (ui *uiState) handleUnitTabInput(gtx layout.Context) {
	ui.ensureUnitClicks()
	// The pickers come first: a click on an open dropdown row belongs to the
	// dropdown, not to the section list or the columns underneath it.
	ui.handleUnitPickerKeys(gtx)
	ui.handleUnitPickerInput(gtx)
	ui.handleUnitRowInput(gtx)
	ui.handleUnitSquadInput(gtx)
	for i := range ui.unitSectionClicks {
		if i < len(unitSections) && ui.unitSectionClicks[i].Clicked(gtx) {
			ui.unitSection = i
			// The section the Object window was left on is part of the
			// saved session along with its pickers.
			ui.markStateDirty()
		}
	}
	// Last, because it acts on the section that was just chosen: a press that
	// left the info section must not be read as a press inside it.
	ui.handleUnitFieldInput(gtx)
	// The mesh eye toggles belong to whichever section shows the mesh list —
	// Events and Classic OE both — so they are drained here rather than
	// inside the Events handler, which returns early for other sections.
	ui.handleUnitMeshToggles(gtx)
	ui.handleUnitEventsInput(gtx)
	ui.handleClassicOEWheInput(gtx)
	ui.handleClassicOEClauseConditionInput(gtx)
}

// ── column geometry ─────────────────────────────────────────────────────────

// unitColumnWidths splits the space into three columns: a fixed width
// menu on the left, a middle column of about a third of the body, and whatever
// is left for the right column. The two minimums keep the columns usable when
// the window is too narrow for the specified proportions, in which case the
// body overflows rather than collapsing a column to nothing.
func unitColumnWidths(bodyW, leftW, gap, minW int) (left, mid, right int) {
	left = leftW
	usable := bodyW - leftW - 2*gap
	mid = usable / 3
	if mid < minW {
		mid = minW
	}
	right = usable - mid
	if right < minW {
		right = minW
	}
	return left, mid, right
}

// layoutUnitBody splits the given space into the three columns. The widths are
// worked out in pixels rather than with flex weights, because the middle column
// is specified as a third of the space and the left one as a fixed menu width.
func (ui *uiState) layoutUnitBody(gtx layout.Context) layout.Dimensions {
	// The columns themselves; the material dialog's dim and card wrap them
	// below, so a double press on a mesh row raises that editor over
	// whichever section is on show — Viewport and Classic OE both.
	columns := func(gtx layout.Context) layout.Dimensions {
		gap := gtx.Dp(unit.Dp(unitColGapDp))
		minW := gtx.Dp(unit.Dp(unitColMinWidthDp))
		// The card's own padding was already consumed by the caller's inset.
		// The caller's own padding was already consumed before this was called.
		leftW, midW, rightW := unitColumnWidths(gtx.Constraints.Max.X, gtx.Dp(unit.Dp(unitLeftColumnDp)), gap, minW)
		if ui.currentUnitSection().label == unitSectionViewport {
			// The 3D view takes the middle and right columns together. A model in a
			// third of the window is a thumbnail, and the cameras the section lists
			// need a column of their own to be picked out of.
			return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
				layout.Rigid(ui.fixedPanel(leftW, ui.layoutUnitLeftColumn)),
				layout.Rigid(layout.Spacer{Width: unit.Dp(gap)}.Layout),
				layout.Flexed(1, ui.layoutUnitViewportArea),
			)
		}
		if ui.currentUnitSection().label == unitSectionClassicOE {
			// The classic editor's layout takes the middle and right columns
			// together for the same reason the Viewport section does: its panel
			// column plus a model preview needs the room, and a properties panel
			// beside a preview would squeeze both.
			return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
				layout.Rigid(ui.fixedPanel(leftW, ui.layoutUnitLeftColumn)),
				layout.Rigid(layout.Spacer{Width: unit.Dp(gap)}.Layout),
				layout.Flexed(1, ui.layoutClassicOEArea),
			)
		}
		return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
			layout.Rigid(ui.fixedPanel(leftW, ui.layoutUnitLeftColumn)),
			layout.Rigid(layout.Spacer{Width: unit.Dp(gap)}.Layout),
			layout.Rigid(ui.fixedPanel(midW, ui.layoutUnitMiddleColumn)),
			layout.Rigid(layout.Spacer{Width: unit.Dp(gap)}.Layout),
			layout.Rigid(ui.fixedPanel(rightW, ui.layoutUnitRightColumn)),
		)
	}
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(columns),
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			if !ui.unitMeshMatOpen {
				return layout.Dimensions{}
			}
			return ui.layoutUnitMeshMatDialog(gtx)
		}),
	)
}

// unitColumn frames one of the three columns: a header strip and a body.
func (ui *uiState) unitColumn(gtx layout.Context, title, hint string) layout.Dimensions {
	return panelFrame(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.layoutUnitColumnHeader(gtx, title)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, cHeaderLine, 1) }),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(12), Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					l := material.Label(ui.th, unit.Sp(10), hint)
					l.Color = cTextDim
					return l.Layout(gtx)
				})
			}),
		)
	})
}

func (ui *uiState) layoutUnitLeftColumn(gtx layout.Context) layout.Dimensions {
	return panelFrame(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.layoutUnitColumnHeader(gtx, "Sections")
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, cHeaderLine, 1) }),
			layout.Flexed(1, ui.layoutUnitSectionList),
		)
	})
}

func (ui *uiState) layoutUnitColumnHeader(gtx layout.Context, title string) layout.Dimensions {
	h := gtx.Dp(unit.Dp(unitColHeaderHeight))
	gtx.Constraints.Min.Y = h
	gtx.Constraints.Max.Y = h
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			sz := image.Pt(gtx.Constraints.Max.X, h)
			fillRect(gtx, cHeaderBg, sz)
			return layout.Dimensions{Size: sz}
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						l := material.Label(ui.th, unit.Sp(11), title)
						l.Color = cText
						l.MaxLines = 1
						return l.Layout(gtx)
					}),
				)
			})
		}),
	)
}

// layoutUnitSectionList is the left menu: one row per section, the active one
// highlighted, keyboard reachable through the row's own clickable. It makes
// sure its click targets exist, since the tab can be drawn without anything
// having opened the tool panel first.
func (ui *uiState) layoutUnitSectionList(gtx layout.Context) layout.Dimensions {
	ui.ensureUnitClicks()
	return layout.Inset{Top: unit.Dp(4), Left: unit.Dp(2), Right: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		var children []layout.FlexChild
		for i := range unitSections {
			i := i
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.layoutUnitSectionRow(gtx, i)
			}))
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	})
}

func (ui *uiState) layoutUnitSectionRow(gtx layout.Context, index int) layout.Dimensions {
	ui.ensureUnitClicks()
	if index < 0 || index >= len(unitSections) {
		return layout.Dimensions{}
	}
	selected := index == ui.unitSection
	bg := cPanelBg
	if selected {
		bg = cSelectBg
	} else if ui.unitSectionClicks[index].Hovered() {
		bg = cMenuHover
	}
	return ui.unitSectionClicks[index].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		h := gtx.Dp(unit.Dp(unitRowHeightDp))
		// The row spans its column, so the width is pinned before the stack
		// lays out: with a zero minimum a background would fall back to the
		// column's maximum height instead of the row height.
		sz := image.Pt(gtx.Constraints.Max.X, h)
		gtx.Constraints.Min = sz
		gtx.Constraints.Max = sz
		return layout.Stack{}.Layout(gtx,
			layout.Expanded(func(gtx layout.Context) layout.Dimensions {
				if bg != cPanelBg {
					rrect(gtx, bg, gtx.Constraints.Max, gtx.Dp(unit.Dp(2)))
				}
				return layout.Dimensions{Size: gtx.Constraints.Max}
			}),
			layout.Stacked(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.th, unit.Sp(11), unitSections[index].label)
							l.Color = cText
							l.MaxLines = 1
							return l.Layout(gtx)
						}),
					)
				})
			}),
		)
	})
}

func (ui *uiState) layoutUnitMiddleColumn(gtx layout.Context) layout.Dimensions {
	return ui.layoutUnitMiddleColumnBody(gtx)
}

func (ui *uiState) layoutUnitRightColumn(gtx layout.Context) layout.Dimensions {
	return ui.layoutUnitRightColumnBody(gtx)
}
