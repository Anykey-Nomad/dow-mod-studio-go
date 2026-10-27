package unit

import (
	"dow-mod-studio/internal/app/uikit"
	"dow-mod-studio/internal/app/uistate"
	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"image"
	"image/color"
	"strings"
)

// DimOverlayColor is the scrim behind a modal window.
var DimOverlayColor = color.NRGBA{R: 0, G: 0, B: 0, A: 90}

// UnitSection describes one entry of the dialog's left menu, together with the
// headings its middle and right columns carry. The headings are what make the
// four views tellable apart before they have any content.
type UnitSection struct {
	Label       string
	MiddleTitle string
	RightTitle  string
	// hint is the placeholder shown in the middle column, stating what will
	// live there once the section is filled in.
	Hint string
}

var UnitSections = []UnitSection{
	{
		Label:       "Unit info",
		MiddleTitle: "Info",
		RightTitle:  "Properties",
		Hint:        "Unit and building properties will be listed here.",
	},
	{
		Label:       "Weapons",
		MiddleTitle: "Weapons",
		RightTitle:  "Weapon properties",
		Hint:        "The unit's weapons will be listed here.",
	},
	{
		Label:       "Abilities",
		MiddleTitle: "Abilities",
		RightTitle:  "Ability properties",
		Hint:        "The unit's abilities will be listed here.",
	},
	{
		Label:       "Squads",
		MiddleTitle: "Squads",
		RightTitle:  "Squad properties",
		Hint:        "The squads this unit belongs to will be listed here.",
	},
	{
		Label:       "Links",
		MiddleTitle: "Links",
		RightTitle:  "Link",
		Hint:        "What this unit refers to, and what refers to it, will be listed here.",
	},
	{
		Label:       "Frozen",
		MiddleTitle: "Frozen units and buildings",
		RightTitle:  "Freeze",
		Hint:        "The units and buildings that have been moved out of the mod, and what was cleared to move them.",
	},
	{
		Label:       "Problems",
		MiddleTitle: "Broken references",
		RightTitle:  "Reference",
		Hint:        "References anywhere in the mod that name a file which is not there.",
	},
	{
		Label:       "Events",
		MiddleTitle: "Timelines",
		RightTitle:  "Events",
		Hint:        "The model's .whe timelines, with the FX and sounds they fire.",
	},
	{
		Label:       "Viewport",
		MiddleTitle: "Viewport",
		RightTitle:  "Model",
		Hint:        "The unit's model, with the animations it can be played with.",
	},
	{
		Label:       UnitSectionClassicOE,
		MiddleTitle: "Classic OE",
		RightTitle:  "Properties",
		Hint:        "The classic object editor's layout: its panel column, with the unit previewed beside it.",
	},
}

// UnitSectionNamed is where a section of the dialog is, by name. It is here
// because a section is referred to by name in more than one place — the freeze
// operation moves the window to the Frozen section when it is done, for one —
// and counting the entries in a slice to get there is how that goes wrong the
// next time a section is added.
func UnitSectionNamed(label string) int {
	for i, s := range UnitSections {
		if s.Label == label {
			return i
		}
	}
	return 0
}

// Geometry of the columns. The left column is sized to read as a menu rather
// than a list, the middle takes a third of the space, and the right column
// takes whatever is left.
const (
	UnitLeftColumnDp    = 132
	UnitColHeaderHeight = 26
	UnitColGapDp        = 8
	UnitColMinWidthDp   = 140
)

// currentUnitSection is the section the dialog is showing, guarded so an index
// left over from a future section list cannot panic the layout.
func CurrentUnitSection(ui *uistate.State) UnitSection {
	if ui.UnitSection < 0 || ui.UnitSection >= len(UnitSections) {
		return UnitSections[0]
	}
	return UnitSections[ui.UnitSection]
}

// UnitToolButtons are the buttons of the small panel that sits in the menu bar
// to the left of the search field. Only the object interface exists so far; the
// list is what the panel iterates over, so another button is one line here.
var UnitToolButtons = []struct {
	Label  string
	Action string // handled in handleUnitToolInput
}{
	{Label: "Object", Action: "unit-interface"},
}

const UnitPanelGapDp = 10

// unitToolButton is one button of the tool panel: a label with its own frame,
// measured the same way because it too sits in a flex row.
func UnitToolButton(ui *uistate.State, gtx layout.Context, btn *widget.Clickable, label string, active bool) layout.Dimensions {
	bg := uikit.CSaveBg
	col := uikit.CSaveBd
	if active {
		bg, col = uikit.CSelectBg, uikit.CSelectBd
	} else if btn.Hovered() {
		bg, col = uikit.CSaveHover, uikit.CSelectBd
	}
	textCol := uikit.CText
	if active {
		textCol = uikit.Rgb(0xFFFFFF)
	}
	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return FrameMeasured(gtx, func(gtx layout.Context) layout.Dimensions {
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
var FrameMeasured = uikit.FrameMeasured

func EnsureUnitClicks(ui *uistate.State) {
	if len(ui.UnitSectionClicks) != len(UnitSections) {
		ui.UnitSectionClicks = make([]widget.Clickable, len(UnitSections))
	}
	if len(ui.UnitToolClicks) != len(UnitToolButtons) {
		ui.UnitToolClicks = make([]widget.Clickable, len(UnitToolButtons))
	}
}

// UnitColumnWidths splits the space into three columns: a fixed width
// menu on the left, a middle column of about a third of the body, and whatever
// is left for the right column. The two minimums keep the columns usable when
// the window is too narrow for the specified proportions, in which case the
// body overflows rather than collapsing a column to nothing.
func UnitColumnWidths(bodyW, leftW, gap, minW int) (left, mid, right int) {
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

// unitColumn frames one of the three columns: a header strip and a body.
func UnitColumn(ui *uistate.State, gtx layout.Context, title, hint string) layout.Dimensions {
	return uikit.PanelFrame(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return LayoutUnitColumnHeader(ui, gtx, title)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return uikit.HLine(gtx, uikit.CHeaderLine, 1) }),
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

func LayoutUnitLeftColumn(ui *uistate.State, gtx layout.Context) layout.Dimensions {
	return uikit.PanelFrame(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return LayoutUnitColumnHeader(ui, gtx, "Sections")
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return uikit.HLine(gtx, uikit.CHeaderLine, 1) }),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return LayoutUnitSectionList(ui, gtx) }),
		)
	})
}

func LayoutUnitColumnHeader(ui *uistate.State, gtx layout.Context, title string) layout.Dimensions {
	h := gtx.Dp(unit.Dp(UnitColHeaderHeight))
	gtx.Constraints.Min.Y = h
	gtx.Constraints.Max.Y = h
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			sz := image.Pt(gtx.Constraints.Max.X, h)
			uikit.FillRect(gtx, uikit.CHeaderBg, sz)
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
func LayoutUnitSectionList(ui *uistate.State, gtx layout.Context) layout.Dimensions {
	EnsureUnitClicks(ui)
	return layout.Inset{Top: unit.Dp(4), Left: unit.Dp(2), Right: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		var children []layout.FlexChild
		for i := range UnitSections {
			i := i
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return LayoutUnitSectionRow(ui, gtx, i)
			}))
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	})
}

func LayoutUnitSectionRow(ui *uistate.State, gtx layout.Context, index int) layout.Dimensions {
	EnsureUnitClicks(ui)
	if index < 0 || index >= len(UnitSections) {
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
		h := gtx.Dp(unit.Dp(uikit.UnitRowHeightDp))
		// The row spans its column, so the width is pinned before the stack
		// lays out: with a zero minimum a background would fall back to the
		// column's maximum height instead of the row height.
		sz := image.Pt(gtx.Constraints.Max.X, h)
		gtx.Constraints.Min = sz
		gtx.Constraints.Max = sz
		return layout.Stack{}.Layout(gtx,
			layout.Expanded(func(gtx layout.Context) layout.Dimensions {
				if bg != uikit.CPanelBg {
					uikit.Rrect(gtx, bg, gtx.Constraints.Max, gtx.Dp(unit.Dp(2)))
				}
				return layout.Dimensions{Size: gtx.Constraints.Max}
			}),
			layout.Stacked(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.Th, unit.Sp(11), UnitSections[index].Label)
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

// ActionIconCommandsDir is the tree folder of the game's command icons.
const ActionIconCommandsDir = `art\ui\ingame\command_icons`

// ActionIconFile maps an action's name to the command icon file (without the
// extension) that best says what the action is, or "" when nothing in the
// vocabulary fits. The match is on lower-cased substrings, most specific
// first: a name that reads "melee" takes the melee icon however the rest of
// it reads, and only a name no earlier rule claimed reaches the generic
// movement and stance pictures at the end.
func ActionIconFile(name string) string {
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
	case ActionIconHasWord(n, "run"), ActionIconHasWord(n, "walk"),
		ActionIconHasWord(n, "chase"), ActionIconHasWord(n, "move"):
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
	case ActionIconHasWord(n, "hold"):
		return "stance_hold"
	case strings.Contains(n, "idle") || strings.Contains(n, "stand"):
		return "stance_stand"
	}
	return ""
}

// ActionIconHasWord reports whether word stands as a whole word inside the
// lower-cased name — "run", "run_in", "idle_run" — so a rule on plain words
// cannot catch "thrown" on its way to some other match.
func ActionIconHasWord(n, word string) bool {
	if n == word || strings.HasPrefix(n, word+"_") || strings.HasSuffix(n, "_"+word) {
		return true
	}
	return strings.Contains(n, "_"+word+"_")
}

// IconCacheEntry is one unit's icon lookup. A miss is cached as well — img is
// nil and tried says the lookup was made — so a unit with no icon is not
// searched for again on every frame.
type IconCacheEntry = uistate.IconCacheEntry

// UnitIconLayout paints img fitted inside a w×h box, preserving its aspect and
// centring it, and reserves the box even when there is no icon — a column of
// rows keeps its text aligned whether or not the mod ships pictures.
func UnitIconLayout(gtx layout.Context, img image.Image, w, h unit.Dp) layout.Dimensions {
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

// FolderIconLayout paints a folder silhouette — a tab raised on the left of a
// wider body — fitted in a w×h box, and reserves the box even when it is
// tiny, so a column of tree rows stays aligned. It marks a row that opens
// into a list, drawn rather than set in type so it looks the same on every
// platform.
func FolderIconLayout(gtx layout.Context, w, h unit.Dp) layout.Dimensions {
	bw, bh := gtx.Dp(w), gtx.Dp(h)
	if bw < 3 || bh < 3 {
		return layout.Dimensions{}
	}
	col := uikit.Rgb(0xE8C14A)
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
