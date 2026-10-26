package app

import (
	"dow-mod-studio/internal/app/uistate"
	"image"
	"path/filepath"
	"strings"

	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/rgd"
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
func LayoutUnitSquadList(ui *uiState, gtx layout.Context) layout.Dimensions {
	d := CurrentUnitDetails(ui)
	if d == nil || d.Error != "" {
		return UnitSectionFrame(ui, gtx, func(gtx layout.Context) layout.Dimensions { return UnitEmptyState(ui, gtx) })
	}
	squads := SquadsOfUnitOf(ui, d.Entry)
	if len(squads) == 0 {
		return UnitSectionFrame(ui, gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(12), Left: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return SquadEmptyMessage(ui, gtx, ui.UnitSquadsDone)
			})
		})
	}
	EnsureUnitRowClicks(ui, len(squads))
	return UnitSectionFrame(ui, gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(6), Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return LayoutListWithScrollbar(ui, gtx, &ui.UnitSquadScroll, &ui.UnitSquadList, gtx.Dp(unit.Dp(squadRowHeightDp)), len(squads), func(gtx layout.Context, index int) layout.Dimensions {
				if index < 0 || index >= len(squads) {
					return layout.Dimensions{}
				}
				return LayoutUnitSquadRow(ui, gtx, index, squads[index])
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
func SquadEmptyMessage(ui *uiState, gtx layout.Context, scanDone bool) layout.Dimensions {
	msg := "This unit is not part of any squad."
	if !scanDone {
		msg = "Looking for the squads this unit belongs to…"
	}
	l := material.Label(ui.Th, unit.Sp(10), msg)
	l.Color = cTextDim
	return l.Layout(gtx)
}

// layoutUnitSquadRow is one squad: its name, and the size the squad is fielded
// at. The size is shown on the row because it is the answer to the question a
// reader opens this column with — a necron lord is 1, a necron warrior is 3 to 8
// — and finding it means opening the squad otherwise.
func LayoutUnitSquadRow(ui *uiState, gtx layout.Context, index int, s unitSquadRef) layout.Dimensions {
	sel := index == ui.UnitSelRow
	bg := cPanelBg
	switch {
	case sel:
		bg = cSelectBg
	case ui.UnitRowClicks[index].Hovered():
		bg = cMenuHover
	}
	return ui.UnitRowClicks[index].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
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
							l := material.Label(ui.Th, unit.Sp(11), name)
							l.Color = cText
							l.MaxLines = 1
							return l.Layout(gtx)
						}),
						layout.Flexed(1, flexGap()),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.Th, unit.Sp(9), squadSizeLabel(s))
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
	min, max := strings.TrimSpace(s.UnitMin), strings.TrimSpace(s.UnitMax)
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
func LayoutUnitSquadRightColumn(ui *uiState, gtx layout.Context, sec unitSection) layout.Dimensions {
	d := CurrentUnitDetails(ui)
	if d == nil || d.Error != "" {
		return UnitColumn(ui, gtx, sec.rightTitle, "No entry is selected.")
	}
	squads := SquadsOfUnitOf(ui, d.Entry)
	if len(squads) == 0 {
		return UnitColumn(ui, gtx, sec.rightTitle, "No entry is selected.")
	}
	idx := clampInt(ui.UnitSelRow, 0, len(squads)-1)
	s := squads[idx]
	fields := squadFields(ui.ModTree, s)
	return panelFrame(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return LayoutUnitColumnHeader(ui, gtx, sec.rightTitle)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, cHeaderLine, 1) }),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(8), Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					children := []layout.FlexChild{
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Bottom: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return UnitKeyValueRow(ui, gtx, "Name", squadNameOf(s), false)
							})
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Bottom: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return UnitKeyValueRow(ui, gtx, "Race", s.Race, false)
							})
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Bottom: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return UnitKeyValueRow(ui, gtx, "File", s.Path, false)
							})
						}),
					}
					for _, f := range fields {
						f := f
						children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Bottom: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return UnitKeyValueRow(ui, gtx, f.Label, f.Value, f.IsLocID)
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
func HandleUnitSquadInput(ui *uiState, gtx layout.Context) {
	if CurrentUnitSection(ui).label != "Squads" {
		return
	}
	d := CurrentUnitDetails(ui)
	if d == nil {
		return
	}
	squads := SquadsOfUnitOf(ui, d.Entry)
	EnsureUnitRowClicks(ui, len(squads))
	for i := range ui.UnitRowClicks {
		if i < len(squads) && ui.UnitRowClicks[i].Clicked(gtx) {
			ui.UnitSelRow = i
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
	if tree == nil || s.Path == "" {
		return nil
	}
	data, err := tree.ReadFile(s.Path)
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
		f := unitField{Group: group, Label: label, Value: value}
		f.StartsGroup = group != "" && (len(out) == 0 || out[len(out)-1].Group != group)
		out = append(out, f)
	}
	// The birth cost is stated first because it is the one number here that is
	// not in the unit's own file, and a mod that charges for a squad puts it
	// there. It is skipped when it is zero, which is what every one of the 431
	// squads of the DXP3 tree carries: the game charges for the troopers through
	// their own cost_ext and leaves birth_resource empty, so five rows of zero
	// would be the whole group on every squad of the base game.
	for _, cur := range birthResourceOrder {
		if s.BirthResource[cur] == "0" {
			continue
		}
		add("Birth cost", birthResourceLabels[cur], s.BirthResource[cur])
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
	return &uiState{UnitSquads: refs, UnitSquadsDone: true}
}

// ─── merged from unit_squads.go ───────────────────────────────────────────────

// ── which squads a unit belongs to ──────────────────────────────────────────
//
// A unit is not a complete object on its own. The .rgd under
// Data\attrib\ebps\races\<race>\troops describes the individual trooper: its
// hit points, its weapons, its model. What the player actually gets is a squad
// of three to eight of them, and that squad is a separate object living under
// Data\attrib\sbps\races\<race>\ named <something>_squad.rgd.
//
// There are no .sbps files in Dawn of War: Definitive Edition, which is worth
// stating because the folder is called sbps and the older game's unit data really
// was .sbps. The "s" side of a unit lives in .rgd files of exactly the same
// shape as the ebps ones, under that folder. Measured across the DXP3 tree: 431
// squad files over 10 races, naming 369 distinct units.
//
// The link runs from the squad to the unit, not the other way round. There is no
// field in a unit's .rgd that names its squad — the only near miss is
// possess_ext\squad_replacement3_name, which names one squad in a bare form and
// only on the 26 units that can be possessed. So the index has to be built by
// walking the squads, which is what this file does.
//
// In squad_loadout_ext:
//
//	trooper_base
//	{
//	    type = "ebps\\races\\necrons\\troops\\necron_lord.lua"
//	}
//
// The reference is relative to Data\attrib and names the .lua the game builds the
// unit into; the file on disk is the .rgd of the same name, which is the same
// trick the ability references use and the same resolveRgdRef handles.
//
// A unit is in at most three squads in the DXP3 tree, and 431 files take about
// 880ms to walk, so the index is built in the background and delivered in small
// batches rather than on the frame the dialog opens.

// squadRacesFolder is where the squad side of a unit lives, relative to the
// tree's "Data" root.
const squadRacesFolder = "attrib\\sbps\\races"

// unitSquadRef is one squad a unit takes part in.
type unitSquadRef = uistate.UnitSquadRef

// unitSquadBatch is a slice of the scan's results. done marks the last one.
type unitSquadBatch = uistate.UnitSquadBatch

// unitSquadBatchSize is how many squads one batch carries.
const unitSquadBatchSize = 24

// startUnitSquadScan walks every squad .rgd in the background and builds the
// index of which squads use which unit. A scan already running for the same tree
// is left alone; one for a different tree is stopped, so a module reload does not
// leave a goroutine reading a tree that is being closed.
func StartUnitSquadScan(ui *uiState, tree *modtree.ModTree) {
	if tree == nil {
		return
	}
	if ui.UnitSquadsRunning && ui.UnitSquadsTree == tree {
		return
	}
	StopUnitSquadScan(ui)

	ui.UnitSquadsTree = tree
	ui.UnitSquads = make(map[string][]unitSquadRef)
	ui.UnitSquadsCh = make(chan unitSquadBatch, 8)
	stop := make(chan struct{})
	ui.UnitSquadsStop = stop
	ui.UnitSquadsRunning = true

	files := collectSquadRgds(tree)
	ch := ui.UnitSquadsCh
	go func() {
		batch := make(map[string][]unitSquadRef)
		flush := func(done bool) bool {
			out := make(map[string][]unitSquadRef, len(batch))
			for k, v := range batch {
				out[k] = v
			}
			select {
			case ch <- unitSquadBatch{ByUnit: out, Done: done}:
				batch = make(map[string][]unitSquadRef)
				return true
			case <-stop:
				return false
			}
		}
		for _, f := range files {
			for _, ref := range squadsOfUnit(tree, f) {
				key := strings.ToLower(ref.UnitRef)
				batch[key] = append(batch[key], ref)
			}
			if len(batch) >= unitSquadBatchSize && !flush(false) {
				return
			}
		}
		flush(true)
	}()
}

// stopUnitSquadScan ends a running scan. The goroutine notices through its stop
// channel and returns, so a module reload does not leave one reading a tree that
// is being closed underneath it.
func StopUnitSquadScan(ui *uiState) {
	if ui.UnitSquadsStop != nil {
		close(ui.UnitSquadsStop)
		ui.UnitSquadsStop = nil
	}
	ui.UnitSquadsRunning = false
	ui.UnitSquadsCh = nil
	ui.UnitSquadsTree = nil
}

// pollUnitSquads takes whatever the scan has finished and reports whether
// anything new arrived, so the caller can ask for a redraw. Nothing else
// animates while a module is idle, so without this the squad column would sit
// empty until the mouse happened to move.
func PollUnitSquads(ui *uiState) bool {
	if ui.UnitSquadsCh == nil {
		return false
	}
	got := false
	for {
		select {
		case b := <-ui.UnitSquadsCh:
			for path, refs := range b.ByUnit {
				// A batch can carry a unit the previous batch also carried, so
				// the lists are concatenated rather than replaced. Deduplication
				// is the reader's job: a squad appears once per trooper_base it
				// declares, and no squad in the DXP3 tree declares more than one.
				ui.UnitSquads[path] = append(ui.UnitSquads[path], refs...)
			}
			if b.Done {
				ui.UnitSquadsDone = true
				StopUnitSquadScan(ui)
			}
			got = true
		default:
			if got {
				SortUnitSquads(ui)
			}
			return got
		}
	}
}

// sortUnitSquads puts every unit's squads in name order, so the list does not
// arrive in whatever order the scan happened to finish in. The scan walks
// races in tree order already, but the batches interleave units, and a list that
// reordered itself between two scans of the same mod would be a bug a reader
// could not explain.
func SortUnitSquads(ui *uiState) {
	for _, refs := range ui.UnitSquads {
		sortSquadRefs(refs)
	}
}

// sortSquadRefs orders one unit's squads by name, with the path as a tiebreak so
// two squads of one name in different races still have a fixed order.
func sortSquadRefs(refs []unitSquadRef) {
	for i := 1; i < len(refs); i++ {
		for j := i; j > 0 && squadRefLess(refs[j], refs[j-1]); j-- {
			refs[j], refs[j-1] = refs[j-1], refs[j]
		}
	}
}

// squadRefLess orders two squads of one unit. The stripped name is the primary
// key, because that is what the row shows; the full name and then the path
// settle the ties, so two squads whose stripped names collide still have a fixed
// order rather than whichever the scan reached first.
func squadRefLess(a, b unitSquadRef) bool {
	if an, bn := squadNameOf(a), squadNameOf(b); an != bn {
		return an < bn
	}
	if a.Name != b.Name {
		return a.Name < b.Name
	}
	return a.Path < b.Path
}

// squadNameOf is a squad's name with the trailing _squad dropped, which is the
// suffix the game's own files end in: necron_lord_squad.rgd is the lord's squad.
//
// Only a suffix is dropped, and only at the end. An earlier version cut at the
// last _squad anywhere, which turned chaos_squad_sorcerer_sp_dxp3 into "chaos" —
// the prefix of the name rather than the squad in it. Leaving a name like
// chaos_squad_sorcerer whole is the lesser evil: it is what the file is called,
// and the row's own file is shown in the right-hand column.
func squadNameOf(s unitSquadRef) string {
	if n, stripped := strings.CutSuffix(s.Name, "_squad"); stripped && n != "" {
		return n
	}
	return s.Name
}

// squadsOfUnit reads one squad file and returns one entry per trooper_base it
// declares, each naming the unit that trooper is. The unit path is carried in
// unitRef rather than returned separately, because the caller keys its batch by
// it and the entry it stores is the enriched copy: a squad that is in the list
// also has a size and a birth cost, and reading the file a second time to get
// those would be a wasted read per squad.
func squadsOfUnit(tree *modtree.ModTree, f unitSquadRef) []unitSquadRef {
	if tree == nil || f.Path == "" {
		return nil
	}
	data, err := tree.ReadFile(f.Path)
	if err != nil {
		return nil
	}
	rf, err := rgd.ParseRgd(data)
	if err != nil {
		return nil
	}
	loadout := rgdFindPath(rf.Root, "squad_loadout_ext")
	if loadout == nil {
		return nil
	}
	// The size and the birth cost belong to the squad rather than to any one
	// trooper, so they are read once here and copied onto every entry.
	base := f
	base.UnitMin = strings.TrimSpace(rgdEntryStr(rgdFindChild(loadout, "unit_min")))
	base.UnitMax = strings.TrimSpace(rgdEntryStr(rgdFindChild(loadout, "unit_max")))
	base.BirthResource = readBirthResource(loadout)

	var out []unitSquadRef
	for _, c := range loadout.Children {
		if c == nil || !c.IsTable() || !strings.EqualFold(c.Name, "trooper_base") {
			continue
		}
		ref := strings.TrimSpace(rgdEntryStr(rgdFindChild(c, "type")))
		if ref == "" {
			continue
		}
		p := resolveRgdRef(tree, ref)
		if p == "" {
			continue
		}
		entry := base
		entry.UnitRef = p
		out = append(out, entry)
	}
	return out
}

// readBirthResource is the squad's birth_resource table: what it costs to raise
// the squad, which is a different cost from the unit's own cost_ext. Only the
// currencies a squad can actually charge are read, and a zero is kept because a
// zero is a real answer.
func readBirthResource(loadout *rgd.RgdEntry) map[string]string {
	br := rgdFindChild(loadout, "birth_resource")
	if br == nil {
		return nil
	}
	out := map[string]string{}
	for _, cur := range []string{"requisition", "power", "pop", "faith", "souls"} {
		if v := strings.TrimSpace(rgdEntryStr(rgdFindChild(br, cur))); v != "" {
			out[cur] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// collectSquadRgds lists every squad file in the tree, sorted by path so the scan
// is reproducible. A race folder that cannot be read is skipped rather than
// failing the whole index, for the same reason the unit catalog does.
func collectSquadRgds(tree *modtree.ModTree) []unitSquadRef {
	var out []unitSquadRef
	if tree == nil || tree.Root == nil {
		return out
	}
	races := modtree.FindFolderByPath(tree.Root, squadRacesFolder)
	if races == nil {
		return out
	}
	for _, race := range races.Children {
		if race == nil {
			continue
		}
		for _, file := range race.Files {
			if file == nil || !strings.EqualFold(filepath.Ext(file.Name), ".rgd") {
				continue
			}
			name := strings.TrimSuffix(file.Name, filepath.Ext(file.Name))
			if name == "" {
				continue
			}
			out = append(out, unitSquadRef{
				Path: file.FullPath,
				Name: name,
				Race: race.Name,
			})
		}
	}
	sortSquadRefs(out)
	return out
}

// squadsOfUnitOf is the dialog's lookup: the squads a given unit is part of, or
// nil when the unit is in none. It is nil rather than an empty slice so a caller
// can tell "not scanned yet" from "this unit is in no squad" — the scan starts
// when the catalog is built and finishes seconds later, and during that window
// every unit would otherwise look like it is in no squad at all.
func SquadsOfUnitOf(ui *uiState, u unitEntry) []unitSquadRef {
	if u.Path == "" || ui.UnitSquads == nil {
		return nil
	}
	return ui.UnitSquads[strings.ToLower(u.Path)]
}
