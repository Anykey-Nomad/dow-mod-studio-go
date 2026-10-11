package main

import (
	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/rgd"
	"image"
	"strings"

	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget/material"
)

// ── the Squads column ───────────────────────────────────────────────────────
//
// The middle column lists the squads a unit is part of and the right one
// describes the selected squad. It works like the weapons and abilities columns
// — a selectable row in the middle, the selected entry's own properties on the
// right — because that is the same question asked of a different kind of thing.
//
// What is different is where the data came from. A weapon or an ability is
// declared inside the unit's own .rgd, so the row is read when the unit is. A
// squad is declared in a file under attrib\sbps\races, and the only link to it
// from the unit's side is the reference in the squad's own loadout, so the rows
// come from the background index rather than from a read.

// squadRowHeightDp is the height of a squad row, the same as a weapon row so the
// three populated columns read as one set.
const squadRowHeightDp = unitRowHeightDp

// layoutUnitSquadList is the middle column of the Squads section.
func (ui *uiState) layoutUnitSquadList(gtx layout.Context) layout.Dimensions {
	d := ui.currentUnitDetails()
	if d == nil || d.error != "" {
		return ui.unitSectionFrame(gtx, ui.unitEmptyState)
	}
	squads := ui.squadsOfUnitOf(d.entry)
	if len(squads) == 0 {
		return ui.unitSectionFrame(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(12), Left: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return ui.squadEmptyMessage(gtx, ui.unitSquadsDone)
			})
		})
	}
	ui.ensureUnitRowClicks(len(squads))
	return ui.unitSectionFrame(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(6), Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return ui.layoutListWithScrollbar(gtx, &ui.unitSquadScroll, &ui.unitSquadList, gtx.Dp(unit.Dp(squadRowHeightDp)), len(squads), func(gtx layout.Context, index int) layout.Dimensions {
				if index < 0 || index >= len(squads) {
					return layout.Dimensions{}
				}
				return ui.layoutUnitSquadRow(gtx, index, squads[index])
			})
		})
	})
}

// squadEmptyMessage says why the list is empty, which is one of two quite
// different things and the reader needs to be told which.
//
// A unit that is genuinely in no squad is a real and common answer: a unit with
// no squad of its own is spawned by a building or an ability rather than fielded.
// A unit whose squads have not arrived yet is only an answer because the index
// is still being built, and saying so is the difference between a reader waiting
// and a reader concluding the unit is orphaned.
func (ui *uiState) squadEmptyMessage(gtx layout.Context, scanDone bool) layout.Dimensions {
	msg := "This unit is not part of any squad."
	if !scanDone {
		msg = "Looking for the squads this unit belongs to…"
	}
	l := material.Label(ui.th, unit.Sp(10), msg)
	l.Color = cTextDim
	return l.Layout(gtx)
}

// layoutUnitSquadRow is one squad: its name, and the size the squad is fielded
// at. The size is shown on the row because it is the answer to the question a
// reader opens this column with — a necron lord is 1, a necron warrior is 3 to 8
// — and finding it means opening the squad otherwise.
func (ui *uiState) layoutUnitSquadRow(gtx layout.Context, index int, s unitSquadRef) layout.Dimensions {
	sel := index == ui.unitSelRow
	bg := cPanelBg
	switch {
	case sel:
		bg = cSelectBg
	case ui.unitRowClicks[index].Hovered():
		bg = cMenuHover
	}
	return ui.unitRowClicks[index].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		h := gtx.Dp(unit.Dp(squadRowHeightDp))
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
							name := squadNameOf(s)
							if len(name) > 30 {
								name = name[:27] + "…"
							}
							l := material.Label(ui.th, unit.Sp(11), name)
							l.Color = cText
							l.MaxLines = 1
							return l.Layout(gtx)
						}),
						layout.Flexed(1, flexGap()),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.th, unit.Sp(9), squadSizeLabel(s))
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

// squadSizeLabel is the unit count of a squad. A squad that fixes its size says
// one number; one that scales says a range. Neither is the default, because the
// game always writes both fields.
func squadSizeLabel(s unitSquadRef) string {
	min, max := strings.TrimSpace(s.unitMin), strings.TrimSpace(s.unitMax)
	switch {
	case min == "" && max == "":
		return ""
	case min == max || min == "":
		return max
	case max == "":
		return min
	case min == "1":
		return "1–" + max
	default:
		return min + "–" + max
	}
}

// layoutUnitSquadRightColumn is the right column: the selected squad's own
// properties, read out of its .rgd. This is the part of the squad the unit's own
// file cannot say anything about — its stance, its morale, its cap cost, how it
// captures — none of which belongs to an individual trooper.
func (ui *uiState) layoutUnitSquadRightColumn(gtx layout.Context, sec unitSection) layout.Dimensions {
	d := ui.currentUnitDetails()
	if d == nil || d.error != "" {
		return ui.unitColumn(gtx, sec.rightTitle, "No entry is selected.")
	}
	squads := ui.squadsOfUnitOf(d.entry)
	if len(squads) == 0 {
		return ui.unitColumn(gtx, sec.rightTitle, "No entry is selected.")
	}
	idx := clampInt(ui.unitSelRow, 0, len(squads)-1)
	s := squads[idx]
	fields := squadFields(ui.modTree, s)
	return panelFrame(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.layoutUnitColumnHeader(gtx, sec.rightTitle)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, cHeaderLine, 1) }),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(8), Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					children := []layout.FlexChild{
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Bottom: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return ui.unitKeyValueRow(gtx, "Name", squadNameOf(s), false)
							})
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Bottom: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return ui.unitKeyValueRow(gtx, "Race", s.race, false)
							})
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Bottom: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return ui.unitKeyValueRow(gtx, "File", s.path, false)
							})
						}),
					}
					for _, f := range fields {
						f := f
						children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Bottom: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return ui.unitKeyValueRow(gtx, f.label, f.value, f.isLocID)
							})
						}))
					}
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
				})
			}),
		)
	})
}

// handleUnitSquadInput owns the Squads column's row clicks. It is separate from
// handleUnitRowInput because the rows come from a different list and a stale
// index from the weapons column must not select a squad.
func (ui *uiState) handleUnitSquadInput(gtx layout.Context) {
	if ui.currentUnitSection().label != "Squads" {
		return
	}
	d := ui.currentUnitDetails()
	if d == nil {
		return
	}
	squads := ui.squadsOfUnitOf(d.entry)
	ui.ensureUnitRowClicks(len(squads))
	for i := range ui.unitRowClicks {
		if i < len(squads) && ui.unitRowClicks[i].Clicked(gtx) {
			ui.unitSelRow = i
			return
		}
	}
}

// squadScalars is the squad's own scalar properties, in the order they are
// shown. Like the unit's table these are measured field names, and the sections
// were read off the 431 squad files of the DXP3 tree rather than guessed.
//
// What is here is what belongs to the squad as a group and not to any trooper in
// it: how many troopers it fields, what it costs to raise, how it stands, how it
// captures, what its morale does, and the cap it takes up.
var squadScalars = []struct {
	group string
	label string
	path  []string
}{
	{"Loadout", "Unit min", []string{"squad_loadout_ext", "unit_min"}},
	{"Loadout", "Unit max", []string{"squad_loadout_ext", "unit_max"}},
	{"Loadout", "Async ability", []string{"squad_loadout_ext", "async_ability"}},
	{"Loadout", "Causes transformation", []string{"squad_loadout_ext", "cause_transformation"}},
	{"Loadout", "Select priority", []string{"squad_loadout_ext", "ui", "multi_select_priority"}},
	{"Loadout", "Counter text id", []string{"squad_loadout_ext", "ui", "counter_text_id"}},
	{"Loadout", "Shows on completion", []string{"squad_ui_ext", "completed_research_display"}},
	{"Loadout", "Icon", []string{"squad_ui_ext", "ui_info", "icon_name"}},
	{"Loadout", "Button hidden", []string{"squad_ui_ext", "ui_info", "no_button"}},

	{"Cap", "Squad cap", []string{"squad_cap_ext", "squad_cap_usage"}},
	{"Cap", "Support cap", []string{"squad_cap_ext", "support_cap_usage"}},

	{"Stance", "Ranged attack range", []string{"squad_combat_stance_ext", "ranged_attackrange"}},
	{"Stance", "Ignores building stance", []string{"squad_combat_stance_ext", "ignore_building_stance"}},
	{"Stance", "No stance button", []string{"squad_combat_stance_ext", "no_stance_button"}},
	{"Stance", "Melee by default", []string{"squad_melee_stance_ext", "default_to_melee"}},

	{"Morale", "Starting morale", []string{"squad_morale_ext", "default"}},
	{"Morale", "Max morale", []string{"squad_morale_ext", "max"}},
	{"Morale", "Rate per second", []string{"squad_morale_ext", "rate_per_second"}},
	{"Morale", "No-combat seconds", []string{"squad_morale_ext", "rate_num_seconds_no_combat"}},
	{"Morale", "Broken at", []string{"squad_morale_ext", "broken_min_morale"}},
	{"Morale", "Broken after", []string{"squad_morale_ext", "broken_min_time"}},
	{"Morale", "Morale armour", []string{"squad_morale_ext", "morale_armour"}},

	{"Capture", "Can capture", []string{"squad_capture_strategic_point_ext", "able_to_capture"}},
	{"Capture", "Can uncapture", []string{"squad_capture_strategic_point_ext", "able_to_uncapture"}},
	{"Capture", "Capture rate", []string{"squad_capture_strategic_point_ext", "capture_rate"}},
	{"Capture", "Time to plant flag", []string{"squad_capture_strategic_point_ext", "time_till_flag_planted"}},

	{"Movement", "Idle formation", []string{"squad_formation_ext", "idle_formation"}},

	{"Transport", "Activates transport", []string{"squad_transportable_ext", "activate_transport_ability"}},
	{"Transport", "Spots", []string{"squad_transportable_ext", "nr_spots"}},

	{"Reinforce", "Can upgrade", []string{"squad_reinforce_ext", "can_upgrade"}},
	{"Reinforce", "Max upgrades", []string{"squad_reinforce_ext", "max_upgrades"}},
	{"Reinforce", "In-combat time multiplier", []string{"squad_reinforce_ext", "in_combat_time_multiplier"}},

	{"Jump", "Jump distance", []string{"squad_jump_ext", "jump_distance_max"}},
	{"Jump", "Jump height", []string{"squad_jump_ext", "jump_height"}},
	{"Jump", "Go up time", []string{"squad_jump_ext", "go_up_time"}},
	{"Jump", "Go down time", []string{"squad_jump_ext", "go_down_time"}},
	{"Jump", "Teleport", []string{"squad_jump_ext", "teleport"}},

	{"Leader", "Max leaders", []string{"squad_leader_ext", "max_leaders"}},
	{"Leader", "Multi with upgrades", []string{"squad_leader_ext", "multi_with_upgrades"}},
	{"Leader", "Multi with reinforce", []string{"squad_leader_ext", "multi_with_reinforce"}},

	{"Infiltration", "Concealed modifiers", []string{"squad_infiltration_ext", "modifiers_while_concealed"}},

	{"Fear", "Fear action time", []string{"squad_fear_ext", "action_time"}},
	{"Fear", "Fear recharge", []string{"squad_fear_ext", "recharge_time"}},
	{"Fear", "Fear title", []string{"squad_fear_ext", "ui_title"}},

	{"Entrench", "Entrenched blueprint", []string{"squad_entrench_ext", "entrenched_squad_blueprint_name"}},
}

// squadCounts are the squad's list properties, counted the same way the unit's
// are: the table ships every slot, so only the slots that say something count.
var squadCounts = []struct {
	group  string
	label  string
	path   []string
	filled func(*rgd.RgdEntry) bool
}{
	{"Lists", "Requirements", []string{"squad_requirement_ext", "requirements"}, requirementSlotUsed},
	{"Lists", "Attack priorities", []string{"squad_combat_stance_ext", "attack_priorities"}, refSlotUsed},
	{"Lists", "Modifiers", []string{"squad_modifier_apply_ext", "modifiers"}, modifierSlotUsed},
	{"Lists", "Broken modifiers", []string{"squad_morale_ext", "broken_modifiers"}, modifierSlotUsed},
	{"Lists", "Leaders", []string{"squad_leader_ext"}, refSlotUsed},
	{"Lists", "Research shown", []string{"squad_ui_ext", "completed_research_filter"}, refSlotUsed},
}

// squadFields reads a squad's own properties, in one read of its .rgd. The rows
// are the squad's, not the unit's, which is the whole point of the column: none of
// them could be read out of the unit's file.
func squadFields(tree *modtree.ModTree, s unitSquadRef) []unitField {
	if tree == nil || s.path == "" {
		return nil
	}
	data, err := tree.ReadFile(s.path)
	if err != nil {
		return nil
	}
	rf, err := rgd.ParseRgd(data)
	if err != nil || rf.Root == nil {
		return nil
	}
	var out []unitField
	add := func(group, label, value string) {
		if strings.TrimSpace(value) == "" {
			return
		}
		f := unitField{group: group, label: label, value: value}
		f.startsGroup = group != "" && (len(out) == 0 || out[len(out)-1].group != group)
		out = append(out, f)
	}
	// The birth cost is stated first because it is the one number here that is
	// not in the unit's own file, and a mod that charges for a squad puts it
	// there. It is skipped when it is zero, which is what every one of the 431
	// squads of the DXP3 tree carries: the game charges for the troopers through
	// their own cost_ext and leaves birth_resource empty, so five rows of zero
	// would be the whole group on every squad of the base game.
	for _, cur := range birthResourceOrder {
		if s.birthResource[cur] == "0" {
			continue
		}
		add("Birth cost", birthResourceLabels[cur], s.birthResource[cur])
	}
	for _, sc := range squadScalars {
		add(sc.group, sc.label, rgdEntryStr(rgdFindPath(rf.Root, sc.path...)))
	}
	for _, c := range squadCounts {
		n, ok := unitListCount(rf.Root, c.path, c.filled)
		if !ok || n == 0 {
			continue
		}
		add(c.group, c.label, itoaCount(n, "entry", "entries"))
	}
	return out
}

// birthResourceOrder is the order the squad's birth cost is shown in, which is
// the order a player reads it: the two currencies every race has, then the ones
// only some do.
var birthResourceOrder = []string{"requisition", "power", "pop", "faith", "souls"}

// birthResourceLabels are the readable names of the birth_resource currencies.
// The file writes pop rather than population, which is the one place the two
// tables disagree on a currency's name.
var birthResourceLabels = map[string]string{
	"requisition": "Requisition",
	"power":       "Power",
	"pop":         "Population",
	"faith":       "Faith",
	"souls":       "Souls",
}

// squadRowsForTest builds a state with a squad index already in place, so a test
// can say what a unit is in without running a scan. It is not used by the app.
func squadRowsForTest(refs map[string][]unitSquadRef) *uiState {
	return &uiState{unitSquads: refs, unitSquadsDone: true}
}
