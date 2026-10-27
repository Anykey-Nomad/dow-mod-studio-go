package app

import (
	"dow-mod-studio/internal/app/uikit"
	"dow-mod-studio/internal/app/uistate"
	"image"
	"image/color"
	"strings"

	"dow-mod-studio/internal/rgd"
	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
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
	unitColHeaderHeight = 26
	unitColGapDp        = 8
	unitColMinWidthDp   = 140
)

// layoutUnitPickerRow places the race and unit strip above the three columns.
// The strip's height is fixed and the dropdowns are drawn over the columns, so
// opening one does not move the columns the user is reading.
func LayoutUnitPickerRow(ui *uiState, gtx layout.Context) layout.Dimensions {
	h := gtx.Dp(unit.Dp(unitPickerStripHeightDp))
	gtx.Constraints.Min.Y = h
	gtx.Constraints.Max.Y = h
	return LayoutUnitPickerStrip(ui, gtx)
}

// unitObjectLabel is what the dialog calls the object it was opened for. It
// names the unit the pickers have selected, because that is what the columns
// below show; the unit's own display name is preferred, and its file name is
// the fallback, since that is the name the game itself uses.
func UnitObjectLabel(ui *uiState) string {
	if d := CurrentUnitDetails(ui); d != nil {
		if d.Name != "" {
			return d.Name
		}
		if d.Entry.Name != "" {
			return d.Entry.Name
		}
	}
	if u := CurrentUnit(ui); u != nil {
		return u.DisplayName()
	}
	return "No unit selected"
}

// currentUnitSection is the section the dialog is showing, guarded so an index
// left over from a future section list cannot panic the layout.
func CurrentUnitSection(ui *uiState) unitSection {
	if ui.UnitSection < 0 || ui.UnitSection >= len(unitSections) {
		return unitSections[0]
	}
	return unitSections[ui.UnitSection]
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
func LayoutUnitPanelSlot(ui *uiState, gtx layout.Context) layout.Dimensions {
	return layout.Inset{Right: unitPanelGapDp, Top: unit.Dp(1), Bottom: unit.Dp(1)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions { return LayoutUnitPanel(ui, gtx) })
}

const unitPanelGapDp = 10

// layoutUnitPanel draws the frame around the tool buttons. It measures its
// content first: the panel is a Rigid in the menu bar's flex row, so its
// constraints still hold all the free width of the bar, and a background sized
// from them would stretch from the buttons to the right edge.
func LayoutUnitPanel(ui *uiState, gtx layout.Context) layout.Dimensions {
	EnsureUnitClicks(ui)
	buttons := func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(2), Bottom: unit.Dp(2), Left: unit.Dp(2), Right: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			var children []layout.FlexChild
			for i := range unitToolButtons {
				i := i
				children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Right: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return UnitToolButton(ui, gtx, &ui.UnitToolClicks[i], unitToolButtons[i].label, UnitTabActive(ui))
					})
				}))
			}
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx, children...)
		})
	}
	return frameMeasured(gtx, buttons, uikit.CSaveBg, uikit.CSaveBd, unit.Dp(3))
}

// unitToolButton is one button of the tool panel: a label with its own frame,
// measured the same way because it too sits in a flex row.
func UnitToolButton(ui *uiState, gtx layout.Context, btn *widget.Clickable, label string, active bool) layout.Dimensions {
	bg := uikit.CSaveBg
	col := uikit.CSaveBd
	if active {
		bg, col = uikit.CSelectBg, uikit.CSelectBd
	} else if btn.Hovered() {
		bg, col = uikit.CSaveHover, uikit.CSelectBd
	}
	textCol := uikit.CText
	if active {
		textCol = rgb(0xFFFFFF)
	}
	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return frameMeasured(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Left: unit.Dp(10), Right: unit.Dp(10), Top: unit.Dp(3), Bottom: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				l := material.Label(ui.Th, unit.Sp(11), label)
				l.Color = textCol
				l.MaxLines = 1
				return l.Layout(gtx)
			})
		}, bg, col, unit.Dp(3))
	})
}

// FrameMeasured lives in uikit; the alias keeps every call site as it was.
var frameMeasured = uikit.FrameMeasured

func EnsureUnitClicks(ui *uiState) {
	if len(ui.UnitSectionClicks) != len(unitSections) {
		ui.UnitSectionClicks = make([]widget.Clickable, len(unitSections))
	}
	if len(ui.UnitToolClicks) != len(unitToolButtons) {
		ui.UnitToolClicks = make([]widget.Clickable, len(unitToolButtons))
	}
}

// ── input ──────────────────────────────────────────────────────────────────

// handleUnitToolInput opens the object interface from the menu bar. It runs on
// every frame, so a click here must not be swallowed by another branch. The
// interface is a tab now, so the button selects or opens that tab rather than
// raising a window.
func HandleUnitToolInput(ui *uiState, gtx layout.Context) {
	EnsureUnitClicks(ui)
	for i := range ui.UnitToolClicks {
		if !ui.UnitToolClicks[i].Clicked(gtx) {
			continue
		}
		switch unitToolButtons[i].action {
		case "unit-interface":
			alreadyOpen := UnitTabActive(ui)
			OpenUnitTab(ui)
			ui.UnitSection = 0
			MarkStateDirty(ui)
			if alreadyOpen {
				// Pressing the button while the tab is on screen would
				// otherwise leave it looking like nothing happened.
				ui.StatusText = "Object interface is already open"
			} else {
				ui.StatusText = "Object interface: " + UnitObjectLabel(ui)
			}
		}
		return
	}
}

// handleUnitTabInput owns the object tab's own clicks: the pickers, the
// selectable rows of the populated columns, and the section list. It runs as
// part of the ordinary input path, so each part consumes only its own clicks.
func HandleUnitTabInput(ui *uiState, gtx layout.Context) {
	EnsureUnitClicks(ui)
	// The pickers come first: a click on an open dropdown row belongs to the
	// dropdown, not to the section list or the columns underneath it.
	HandleUnitPickerKeys(ui, gtx)
	HandleUnitPickerInput(ui, gtx)
	HandleUnitRowInput(ui, gtx)
	HandleUnitSquadInput(ui, gtx)
	for i := range ui.UnitSectionClicks {
		if i < len(unitSections) && ui.UnitSectionClicks[i].Clicked(gtx) {
			ui.UnitSection = i
			// The section the Object window was left on is part of the
			// saved session along with its pickers.
			MarkStateDirty(ui)
		}
	}
	// Last, because it acts on the section that was just chosen: a press that
	// left the info section must not be read as a press inside it.
	HandleUnitFieldInput(ui, gtx)
	// The mesh eye toggles belong to whichever section shows the mesh list —
	// Events and Classic OE both — so they are drained here rather than
	// inside the Events handler, which returns early for other sections.
	HandleUnitMeshToggles(ui, gtx)
	HandleUnitEventsInput(ui, gtx)
	HandleClassicOEWheInput(ui, gtx)
	HandleClassicOEClauseConditionInput(ui, gtx)
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
func LayoutUnitBody(ui *uiState, gtx layout.Context) layout.Dimensions {
	// The columns themselves; the material dialog's dim and card wrap them
	// below, so a double press on a mesh row raises that editor over
	// whichever section is on show — Viewport and Classic OE both.
	columns := func(gtx layout.Context) layout.Dimensions {
		gap := gtx.Dp(unit.Dp(unitColGapDp))
		minW := gtx.Dp(unit.Dp(unitColMinWidthDp))
		// The card's own padding was already consumed by the caller's inset.
		// The caller's own padding was already consumed before this was called.
		leftW, midW, rightW := unitColumnWidths(gtx.Constraints.Max.X, gtx.Dp(unit.Dp(unitLeftColumnDp)), gap, minW)
		if CurrentUnitSection(ui).label == unitSectionViewport {
			// The 3D view takes the middle and right columns together. A model in a
			// third of the window is a thumbnail, and the cameras the section lists
			// need a column of their own to be picked out of.
			return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
				layout.Rigid(FixedPanel(ui, leftW, func(gtx layout.Context) layout.Dimensions { return LayoutUnitLeftColumn(ui, gtx) })),
				layout.Rigid(layout.Spacer{Width: unit.Dp(gap)}.Layout),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return LayoutUnitViewportArea(ui, gtx) }),
			)
		}
		if CurrentUnitSection(ui).label == unitSectionClassicOE {
			// The classic editor's layout takes the middle and right columns
			// together for the same reason the Viewport section does: its panel
			// column plus a model preview needs the room, and a properties panel
			// beside a preview would squeeze both.
			return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
				layout.Rigid(FixedPanel(ui, leftW, func(gtx layout.Context) layout.Dimensions { return LayoutUnitLeftColumn(ui, gtx) })),
				layout.Rigid(layout.Spacer{Width: unit.Dp(gap)}.Layout),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return LayoutClassicOEArea(ui, gtx) }),
			)
		}
		return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
			layout.Rigid(FixedPanel(ui, leftW, func(gtx layout.Context) layout.Dimensions { return LayoutUnitLeftColumn(ui, gtx) })),
			layout.Rigid(layout.Spacer{Width: unit.Dp(gap)}.Layout),
			layout.Rigid(FixedPanel(ui, midW, func(gtx layout.Context) layout.Dimensions { return LayoutUnitMiddleColumn(ui, gtx) })),
			layout.Rigid(layout.Spacer{Width: unit.Dp(gap)}.Layout),
			layout.Rigid(FixedPanel(ui, rightW, func(gtx layout.Context) layout.Dimensions { return LayoutUnitRightColumn(ui, gtx) })),
		)
	}
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(columns),
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			if !ui.UnitMeshMatOpen {
				return layout.Dimensions{}
			}
			return LayoutUnitMeshMatDialog(ui, gtx)
		}),
	)
}

// unitColumn frames one of the three columns: a header strip and a body.
func UnitColumn(ui *uiState, gtx layout.Context, title, hint string) layout.Dimensions {
	return panelFrame(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return LayoutUnitColumnHeader(ui, gtx, title)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, uikit.CHeaderLine, 1) }),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(12), Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					l := material.Label(ui.Th, unit.Sp(10), hint)
					l.Color = uikit.CTextDim
					return l.Layout(gtx)
				})
			}),
		)
	})
}

func LayoutUnitLeftColumn(ui *uiState, gtx layout.Context) layout.Dimensions {
	return panelFrame(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return LayoutUnitColumnHeader(ui, gtx, "Sections")
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, uikit.CHeaderLine, 1) }),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return LayoutUnitSectionList(ui, gtx) }),
		)
	})
}

func LayoutUnitColumnHeader(ui *uiState, gtx layout.Context, title string) layout.Dimensions {
	h := gtx.Dp(unit.Dp(unitColHeaderHeight))
	gtx.Constraints.Min.Y = h
	gtx.Constraints.Max.Y = h
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			sz := image.Pt(gtx.Constraints.Max.X, h)
			fillRect(gtx, uikit.CHeaderBg, sz)
			return layout.Dimensions{Size: sz}
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						l := material.Label(ui.Th, unit.Sp(11), title)
						l.Color = uikit.CText
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
func LayoutUnitSectionList(ui *uiState, gtx layout.Context) layout.Dimensions {
	EnsureUnitClicks(ui)
	return layout.Inset{Top: unit.Dp(4), Left: unit.Dp(2), Right: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		var children []layout.FlexChild
		for i := range unitSections {
			i := i
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return LayoutUnitSectionRow(ui, gtx, i)
			}))
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	})
}

func LayoutUnitSectionRow(ui *uiState, gtx layout.Context, index int) layout.Dimensions {
	EnsureUnitClicks(ui)
	if index < 0 || index >= len(unitSections) {
		return layout.Dimensions{}
	}
	selected := index == ui.UnitSection
	bg := uikit.CPanelBg
	if selected {
		bg = uikit.CSelectBg
	} else if ui.UnitSectionClicks[index].Hovered() {
		bg = uikit.CMenuHover
	}
	return ui.UnitSectionClicks[index].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		h := gtx.Dp(unit.Dp(unitRowHeightDp))
		// The row spans its column, so the width is pinned before the stack
		// lays out: with a zero minimum a background would fall back to the
		// column's maximum height instead of the row height.
		sz := image.Pt(gtx.Constraints.Max.X, h)
		gtx.Constraints.Min = sz
		gtx.Constraints.Max = sz
		return layout.Stack{}.Layout(gtx,
			layout.Expanded(func(gtx layout.Context) layout.Dimensions {
				if bg != uikit.CPanelBg {
					rrect(gtx, bg, gtx.Constraints.Max, gtx.Dp(unit.Dp(2)))
				}
				return layout.Dimensions{Size: gtx.Constraints.Max}
			}),
			layout.Stacked(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.Th, unit.Sp(11), unitSections[index].label)
							l.Color = uikit.CText
							l.MaxLines = 1
							return l.Layout(gtx)
						}),
					)
				})
			}),
		)
	})
}

func LayoutUnitMiddleColumn(ui *uiState, gtx layout.Context) layout.Dimensions {
	return LayoutUnitMiddleColumnBody(ui, gtx)
}

func LayoutUnitRightColumn(ui *uiState, gtx layout.Context) layout.Dimensions {
	return LayoutUnitRightColumnBody(ui, gtx)
}

// ─── merged from unit_action_icons.go ───────────────────────────────────────────────

// ── action icons ────────────────────────────────────────────────────────────
//
// The game's own command buttons double as a vocabulary for the Classic
// Object Editor's action rows: Data\art\ui\ingame\command_icons holds a tga
// per command (move, melee, capturepoint, stance_*), so an action whose name
// says what it does can borrow the picture the game itself would draw for
// that command. actionIconFile works the whole mapping out from the name —
// nothing here reads the .whe — and an action the vocabulary does not cover
// simply gets no icon.
//
// Icons decode once and stay for as long as the mod is open, keyed by the
// action's lower-cased name: the tree asks for the icons of its visible rows
// on every frame, and decoding a .tga per frame is not that.

// actionIconCommandsDir is the tree folder of the game's command icons.
const actionIconCommandsDir = `art\ui\ingame\command_icons`

// actionIcon returns the decoded command icon of an action or motion, or nil
// when the game's vocabulary has none for it.
func ActionIcon(ui *uiState, name string) image.Image {
	file := actionIconFile(name)
	if file == "" || ui.ModTree == nil {
		return nil
	}
	mod := ui.ModTree.FileMapName
	if ui.ActionIcons == nil || ui.ActionIconsMod != mod {
		ui.ActionIcons = make(map[string]iconCacheEntry)
		ui.ActionIconsMod = mod
	}
	key := strings.ToLower(strings.TrimSpace(name))
	if e, ok := ui.ActionIcons[key]; ok {
		return e.Img
	}
	img := ReadIconAt(ui, actionIconCommandsDir+`\`+file)
	ui.ActionIcons[key] = iconCacheEntry{Img: img, Tried: true}
	return img
}

// actionIconFile maps an action's name to the command icon file (without the
// extension) that best says what the action is, or "" when nothing in the
// vocabulary fits. The match is on lower-cased substrings, most specific
// first: a name that reads "melee" takes the melee icon however the rest of
// it reads, and only a name no earlier rule claimed reaches the generic
// movement and stance pictures at the end.
func actionIconFile(name string) string {
	n := strings.ToLower(strings.TrimSpace(name))
	switch {
	case n == "":
		return ""
	case strings.Contains(n, "holdfire") || strings.Contains(n, "hold_fire") || strings.Contains(n, "cease"):
		return "stance_cease"
	case strings.Contains(n, "melee"):
		// Before every attack, charge and move rule: a melee
		// whatever is still a melee whatever.
		return "melee"
	case strings.Contains(n, "capture"):
		return "capturepoint"
	case strings.Contains(n, "garrison"):
		return "capturepoint"
	case strings.Contains(n, "repair"):
		return "repair"
	case strings.Contains(n, "rally"):
		return "rallypoint"
	case strings.Contains(n, "stop"):
		return "stop"
	case strings.Contains(n, "detach"):
		return "detach"
	case strings.Contains(n, "attach"):
		return "attach"
	case strings.Contains(n, "undeploy"):
		return "undeploy_weapon"
	case strings.Contains(n, "deploy"):
		return "deploy_weapon"
	case strings.Contains(n, "unload_here"):
		return "unload_here"
	case strings.Contains(n, "unload"):
		return "unloading"
	case strings.Contains(n, "build"):
		if strings.Contains(n, "advanced") || strings.Contains(n, "upgrade") {
			return "build_structure_advanced"
		}
		return "build_structure"
	case strings.Contains(n, "attack_move"):
		return "attackm"
	case strings.Contains(n, "attack"):
		return "attackg"
	case strings.Contains(n, "infiltrate"):
		return "infiltrate_on"
	case strings.Contains(n, "teleport") || strings.Contains(n, "phase"):
		return "teleport"
	case strings.Contains(n, "jump") || strings.Contains(n, "leap"):
		return "jumping"
	case strings.Contains(n, "entrench"):
		return "entrench"
	case strings.Contains(n, "cannibalize"):
		return "cannibalize"
	case strings.Contains(n, "possess"):
		return "possess"
	case strings.Contains(n, "scuttle"):
		return "scuttle"
	case strings.Contains(n, "reinforce"):
		return "reinforce"
	case strings.Contains(n, "uproot"):
		return "uproot"
	case strings.Contains(n, "deepstrike") || strings.Contains(n, "deep_strike"):
		return "deepstrike"
	case strings.Contains(n, "droppod") || strings.Contains(n, "drop_pod"):
		return "droppod"
	case strings.Contains(n, "summon"):
		return "summoning"
	case strings.Contains(n, "rampage"):
		return "rampage"
	case strings.Contains(n, "train"):
		return "train_units"
	case strings.Contains(n, "bomb"):
		return "place_bomb"
	case strings.Contains(n, "cancel"):
		return "cancel"
	case strings.Contains(n, "charge") || strings.Contains(n, "patrol"):
		return "move"
	case actionIconHasWord(n, "run"), actionIconHasWord(n, "walk"),
		actionIconHasWord(n, "chase"), actionIconHasWord(n, "move"):
		return "move"
	case strings.Contains(n, "sprint"):
		return "move"
	case strings.Contains(n, "stance_"):
		switch {
		case strings.Contains(n, "hold"):
			return "stance_hold"
		case strings.Contains(n, "assault"):
			return "stance_assault"
		case strings.Contains(n, "burn"):
			return "stance_burn"
		case strings.Contains(n, "ranged"), strings.Contains(n, "range"):
			return "stance_ranged"
		case strings.Contains(n, "attack"), strings.Contains(n, "fire"):
			return "stance_attack"
		}
		return "stance_stand"
	case actionIconHasWord(n, "hold"):
		return "stance_hold"
	case strings.Contains(n, "idle") || strings.Contains(n, "stand"):
		return "stance_stand"
	}
	return ""
}

// actionIconHasWord reports whether word stands as a whole word inside the
// lower-cased name — "run", "run_in", "idle_run" — so a rule on plain words
// cannot catch "thrown" on its way to some other match.
func actionIconHasWord(n, word string) bool {
	if n == word || strings.HasPrefix(n, word+"_") || strings.HasSuffix(n, "_"+word) {
		return true
	}
	return strings.Contains(n, "_"+word+"_")
}

// ─── merged from unit_icon.go ───────────────────────────────────────────────

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
type iconCacheEntry = uistate.IconCacheEntry

// unitIcon returns the decoded icon of a unit or building, or nil when the mod
// has none.
func UnitIcon(ui *uiState, u unitEntry) image.Image {
	if ui.ModTree == nil || u.Path == "" {
		return nil
	}
	mod := ui.ModTree.FileMapName
	if ui.UnitIcons == nil || ui.UnitIconsMod != mod {
		ui.UnitIcons = make(map[string]iconCacheEntry)
		ui.UnitIconsMod = mod
	}
	if e, ok := ui.UnitIcons[u.Path]; ok {
		return e.Img
	}
	img := LoadUnitIcon(ui, u)
	ui.UnitIcons[u.Path] = iconCacheEntry{Img: img, Tried: true}
	return img
}

// loadUnitIcon resolves and decodes the icon of one unit, cheapest lookup
// first: the game's naming convention, then the file anywhere in the tree,
// then the name the unit's own .rgd records — reading the unit is the
// expensive part, so it is last.
func LoadUnitIcon(ui *uiState, u unitEntry) image.Image {
	if ui.ModTree == nil {
		return nil
	}
	stem := unitTemplateIconBase(u.Race) + `\` + u.Name + "_icon"
	if img := ReadIconAt(ui, `art\ui\ingame\`+stem); img != nil {
		return img
	}
	if img := ReadIconByName(ui, u.Name+"_icon"); img != nil {
		return img
	}
	data, err := ui.ModTree.ReadFile(u.Path)
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
	if img := ReadIconAt(ui, `art\ui\ingame\`+icon); img != nil {
		return img
	}
	// icon_name carries the race folder in front ("necrons/necron_icons/...")
	// while the file sits directly under the icons folder.
	if i := strings.Index(icon, `\`); i >= 0 {
		if img := ReadIconAt(ui, `art\ui\ingame\`+icon[i+1:]); img != nil {
			return img
		}
	}
	if i := strings.LastIndex(icon, `\`); i >= 0 {
		icon = icon[i+1:]
	}
	return ReadIconByName(ui, icon)
}

// readIconAt looks for the given tree path without an extension, trying the
// two forms an icon ships in.
func ReadIconAt(ui *uiState, virtualPathNoExt string) image.Image {
	for _, ext := range []string{".tga", ".rsh"} {
		data, err := ui.ModTree.ReadFile(virtualPathNoExt + ext)
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
func ReadIconByName(ui *uiState, stem string) image.Image {
	for _, ext := range []string{".tga", ".rsh"} {
		f := ui.ModTree.FindByName(stem + ext)
		if f == nil {
			continue
		}
		data, err := ui.ModTree.ReadFile(f.FullPath)
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

// folderIconLayout paints a folder silhouette — a tab raised on the left of a
// wider body — fitted in a w×h box, and reserves the box even when it is
// tiny, so a column of tree rows stays aligned. It marks a row that opens
// into a list, drawn rather than set in type so it looks the same on every
// platform.
func folderIconLayout(gtx layout.Context, w, h unit.Dp) layout.Dimensions {
	bw, bh := gtx.Dp(w), gtx.Dp(h)
	if bw < 3 || bh < 3 {
		return layout.Dimensions{}
	}
	col := rgb(0xE8C14A)
	fill := func(r image.Rectangle) {
		st := clip.Rect(r).Push(gtx.Ops)
		paint.ColorOp{Color: col}.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
		st.Pop()
	}
	tabW := (bw*2 + 2) / 5  // the tab runs about two fifths across
	tabH := (bh*3 + 5) / 10 // and about a third of the height
	if tabW < 2 {
		tabW = 2
	}
	if tabH < 1 {
		tabH = 1
	}
	// The body spans the full width below the tab; the tab sits above it on
	// the left and overlaps by a pixel so the two read as one silhouette
	// with no hairline between them.
	fill(image.Rect(0, tabH, bw, bh))
	fill(image.Rect(0, 0, tabW, tabH+1))
	return layout.Dimensions{Size: image.Pt(bw, bh)}
}
