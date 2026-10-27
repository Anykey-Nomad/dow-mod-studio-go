package unit

import (
	"dow-mod-studio/internal/app/uikit"
	"dow-mod-studio/internal/app/uistate"
	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/rgd"
	"dow-mod-studio/internal/unitdef"
	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget/material"
	"path/filepath"
	"strings"
)

// SquadRowHeightDp is the height of a squad row, the same as a weapon row so the
// three populated columns read as one set.
const SquadRowHeightDp = uikit.UnitRowHeightDp

// squadEmptyMessage says why the list is empty, which is one of two quite
// different things and the reader needs to be told which.
//
// A unit that is genuinely in no squad is a real and common answer: a unit with
// no squad of its own is spawned by a building or an ability rather than fielded.
// A unit whose squads have not arrived yet is only an answer because the index
// is still being built, and saying so is the difference between a reader waiting
// and a reader concluding the unit is orphaned.
func SquadEmptyMessage(ui *uistate.State, gtx layout.Context, scanDone bool) layout.Dimensions {
	msg := "This unit is not part of any squad."
	if !scanDone {
		msg = "Looking for the squads this unit belongs to…"
	}
	l := material.Label(ui.Th, unit.Sp(10), msg)
	l.Color = uikit.CTextDim
	return l.Layout(gtx)
}

// SquadSizeLabel is the unit count of a squad. A squad that fixes its size says
// one number; one that scales says a range. Neither is the default, because the
// game always writes both fields.
func SquadSizeLabel(s UnitSquadRef) string {
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

// SquadScalars is the squad's own scalar properties, in the order they are
// shown. Like the unit's table these are measured field names, and the sections
// were read off the 431 squad files of the DXP3 tree rather than guessed.
//
// What is here is what belongs to the squad as a group and not to any trooper in
// it: how many troopers it fields, what it costs to raise, how it stands, how it
// captures, what its morale does, and the cap it takes up.
var SquadScalars = []struct {
	Group string
	Label string
	Path  []string
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

// BirthResourceOrder is the order the squad's birth cost is shown in, which is
// the order a player reads it: the two currencies every race has, then the ones
// only some do.
var BirthResourceOrder = []string{"requisition", "power", "pop", "faith", "souls"}

// BirthResourceLabels are the readable names of the birth_resource currencies.
// The file writes pop rather than population, which is the one place the two
// tables disagree on a currency's name.
var BirthResourceLabels = map[string]string{
	"requisition": "Requisition",
	"power":       "Power",
	"pop":         "Population",
	"faith":       "Faith",
	"souls":       "Souls",
}

// SquadRowsForTest builds a state with a squad index already in place, so a test
// can say what a unit is in without running a scan. It is not used by the app.
func SquadRowsForTest(refs map[string][]UnitSquadRef) *uistate.State {
	return &uistate.State{UnitSquads: refs, UnitSquadsDone: true}
}

// SquadRacesFolder is where the squad side of a unit lives, relative to the
// tree's "Data" root.
const SquadRacesFolder = "attrib\\sbps\\races"

// UnitSquadRef is one squad a unit takes part in.
type UnitSquadRef = uistate.UnitSquadRef

// UnitSquadBatch is a slice of the scan's results. done marks the last one.
type UnitSquadBatch = uistate.UnitSquadBatch

// UnitSquadBatchSize is how many squads one batch carries.
const UnitSquadBatchSize = 24

// stopUnitSquadScan ends a running scan. The goroutine notices through its stop
// channel and returns, so a module reload does not leave one reading a tree that
// is being closed underneath it.
func StopUnitSquadScan(ui *uistate.State) {
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
func PollUnitSquads(ui *uistate.State) bool {
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
func SortUnitSquads(ui *uistate.State) {
	for _, refs := range ui.UnitSquads {
		SortSquadRefs(refs)
	}
}

// SortSquadRefs orders one unit's squads by name, with the path as a tiebreak so
// two squads of one name in different races still have a fixed order.
func SortSquadRefs(refs []UnitSquadRef) {
	for i := 1; i < len(refs); i++ {
		for j := i; j > 0 && SquadRefLess(refs[j], refs[j-1]); j-- {
			refs[j], refs[j-1] = refs[j-1], refs[j]
		}
	}
}

// SquadRefLess orders two squads of one unit. The stripped name is the primary
// key, because that is what the row shows; the full name and then the path
// settle the ties, so two squads whose stripped names collide still have a fixed
// order rather than whichever the scan reached first.
func SquadRefLess(a, b UnitSquadRef) bool {
	if an, bn := SquadNameOf(a), SquadNameOf(b); an != bn {
		return an < bn
	}
	if a.Name != b.Name {
		return a.Name < b.Name
	}
	return a.Path < b.Path
}

// SquadNameOf is a squad's name with the trailing _squad dropped, which is the
// suffix the game's own files end in: necron_lord_squad.rgd is the lord's squad.
//
// Only a suffix is dropped, and only at the end. An earlier version cut at the
// last _squad anywhere, which turned chaos_squad_sorcerer_sp_dxp3 into "chaos" —
// the prefix of the name rather than the squad in it. Leaving a name like
// chaos_squad_sorcerer whole is the lesser evil: it is what the file is called,
// and the row's own file is shown in the right-hand column.
func SquadNameOf(s UnitSquadRef) string {
	if n, stripped := strings.CutSuffix(s.Name, "_squad"); stripped && n != "" {
		return n
	}
	return s.Name
}

// ReadBirthResource is the squad's birth_resource table: what it costs to raise
// the squad, which is a different cost from the unit's own cost_ext. Only the
// currencies a squad can actually charge are read, and a zero is kept because a
// zero is a real answer.
func ReadBirthResource(loadout *rgd.RgdEntry) map[string]string {
	br := unitdef.RgdFindChild(loadout, "birth_resource")
	if br == nil {
		return nil
	}
	out := map[string]string{}
	for _, cur := range []string{"requisition", "power", "pop", "faith", "souls"} {
		if v := strings.TrimSpace(unitdef.RgdEntryStr(unitdef.RgdFindChild(br, cur))); v != "" {
			out[cur] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// CollectSquadRgds lists every squad file in the tree, sorted by path so the scan
// is reproducible. A race folder that cannot be read is skipped rather than
// failing the whole index, for the same reason the unit catalog does.
func CollectSquadRgds(tree *modtree.ModTree) []UnitSquadRef {
	var out []UnitSquadRef
	if tree == nil || tree.Root == nil {
		return out
	}
	races := modtree.FindFolderByPath(tree.Root, SquadRacesFolder)
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
			out = append(out, UnitSquadRef{
				Path: file.FullPath,
				Name: name,
				Race: race.Name,
			})
		}
	}
	SortSquadRefs(out)
	return out
}

// squadsOfUnitOf is the dialog's lookup: the squads a given unit is part of, or
// nil when the unit is in none. It is nil rather than an empty slice so a caller
// can tell "not scanned yet" from "this unit is in no squad" — the scan starts
// when the catalog is built and finishes seconds later, and during that window
// every unit would otherwise look like it is in no squad at all.
func SquadsOfUnitOf(ui *uistate.State, u unitdef.UnitEntry) []UnitSquadRef {
	if u.Path == "" || ui.UnitSquads == nil {
		return nil
	}
	return ui.UnitSquads[strings.ToLower(u.Path)]
}
