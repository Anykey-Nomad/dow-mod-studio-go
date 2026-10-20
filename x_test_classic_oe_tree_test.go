package main

import (
	"strconv"
	"testing"

	"dow-mod-studio/internal/whe"
)

// The Action Tree folder is derived, not stored: roots are the actions no
// other action calls, called actions hang under their caller, and motions
// are leaves beside them. The panel only flattens that structure, so these
// tests pin the derivation itself — the flat DATAACTS list of a .whe has to
// become the hierarchy the classic editor drew.

// treeNames lists a row dump as "name@depth" for readable diffs.
func treeNames(rows []classicOETreeRow) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.Name + "@" + strconv.Itoa(r.Depth)
	}
	return out
}

func TestClassicOETreeRoots(t *testing.T) {
	actions := []whe.FxAction{
		{Name: "default", Subactions: []whe.FxActionLink{
			{Name: "IdleFire", Compare: whe.FxCompareIf, Condition: "can_fire"},
			{Name: "movefire", Compare: whe.FxCompareElse},
		}},
		{Name: "idlefire"}, // called with different case — still not a root
		{Name: "movefire"},
		{Name: "capture_strategic_point"},
	}
	roots := classicOETreeRoots(actions)
	want := []string{"default", "capture_strategic_point"}
	if len(roots) != len(want) {
		t.Fatalf("roots = %d, want %d (%v)", len(roots), len(want), roots)
	}
	for i, name := range want {
		if roots[i].Name != name {
			t.Errorf("root %d = %q, want %q", i, roots[i].Name, name)
		}
	}
}

func TestClassicOETreeRowsDefaults(t *testing.T) {
	actions := []whe.FxAction{
		{Name: "default", Subactions: []whe.FxActionLink{
			{Name: "idlefire", Compare: whe.FxCompareIf, Condition: "can_fire"},
			{Name: "movefire", Compare: whe.FxCompareElse},
		}, Motions: []whe.FxActionLink{
			{Name: "move", Compare: whe.FxCompareNone},
		}},
		{Name: "idlefire", Motions: []whe.FxActionLink{
			{Name: "idle_fire_devourer", Compare: whe.FxCompareNone},
		}},
		{Name: "movefire", Motions: []whe.FxActionLink{
			{Name: "runaim_d", Compare: whe.FxCompareElse},
		}},
	}
	// With no expansion state recorded: roots open, nested nodes closed —
	// so the root's children and its motion leaves show, but a closed
	// sub-action keeps its own motions hidden.
	rows := classicOETreeRows(actions, nil)
	want := []classicOETreeRow{
		{Name: "default", Depth: 0, Key: "/default", Expanded: true},
		{Name: "idlefire", Depth: 1, Note: "if can_fire", Key: "/default/idlefire", Expanded: false},
		{Name: "movefire", Depth: 1, Note: "else", Key: "/default/movefire", Expanded: false},
		{Name: "move", Depth: 1, Key: ""},
	}
	if len(rows) != len(want) {
		t.Fatalf("rows = %v, want %v", treeNames(rows), treeNames(want))
	}
	for i := range want {
		if rows[i] != want[i] {
			t.Errorf("row %d = %+v, want %+v", i, rows[i], want[i])
		}
	}
}

func TestClassicOETreeRowsExpansion(t *testing.T) {
	actions := []whe.FxAction{
		{Name: "default", Subactions: []whe.FxActionLink{
			{Name: "idlefire", Compare: whe.FxCompareNone},
		}},
		{Name: "idlefire", Motions: []whe.FxActionLink{
			{Name: "idle_fire_devourer", Compare: whe.FxCompareNone},
		}},
	}
	// An explicit open node shows its children regardless of the default.
	rows := classicOETreeRows(actions, map[string]bool{"/default/idlefire": true})
	got := treeNames(rows)
	want := []string{"default@0", "idlefire@1", "idle_fire_devourer@2"}
	if len(got) != len(want) {
		t.Fatalf("rows = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d = %v, want %v", i, got[i], want[i])
		}
	}
	if !rows[1].Expanded {
		t.Errorf("idlefire Expanded = false, want true")
	}

	// An explicit closed root collapses everything beneath it.
	rows = classicOETreeRows(actions, map[string]bool{"/default": false})
	if len(rows) != 1 {
		t.Fatalf("rows = %v, want only the collapsed root", treeNames(rows))
	}
	if rows[0].Expanded {
		t.Errorf("default Expanded = true, want false")
	}
}

func TestClassicOETreeRowsCycleTerminates(t *testing.T) {
	actions := []whe.FxAction{
		{Name: "a", Subactions: []whe.FxActionLink{{Name: "b", Compare: whe.FxCompareNone}}},
		{Name: "b", Subactions: []whe.FxActionLink{{Name: "a", Compare: whe.FxCompareNone}}},
	}
	// Both nodes forced open: a calls b calls a. The link back up the tree
	// has to appear as an inert leaf instead of recursing forever.
	rows := classicOETreeRows(actions, map[string]bool{"/a": true, "/a/b": true})
	got := treeNames(rows)
	want := []string{"a@0", "b@1", "a@2"}
	if len(got) != len(want) {
		t.Fatalf("rows = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d = %v, want %v", i, got[i], want[i])
		}
	}
	if rows[2].Key != "" {
		t.Errorf("cyclic leaf Key = %q, want \"\" (it must not be expandable)", rows[2].Key)
	}
}

func TestClassicOETreeJumpIndex(t *testing.T) {
	clips := []string{"die_1", "idle_1", "move", "runaim_d"}
	actions := []whe.FxAction{
		{Name: "default", Subactions: []whe.FxActionLink{
			{Name: "idlefire", Compare: whe.FxCompareNone},
		}, Motions: []whe.FxActionLink{
			{Name: "Move", Compare: whe.FxCompareNone},
		}},
		{Name: "idlefire", Motions: []whe.FxActionLink{
			{Name: "idle_1", Compare: whe.FxCompareNone},
		}},
		{Name: "die", Motions: []whe.FxActionLink{
			{Name: "die_1", Compare: whe.FxCompareNone},
		}},
		{Name: "loop", Subactions: []whe.FxActionLink{
			{Name: "loop", Compare: whe.FxCompareNone},
		}},
	}

	// A leaf matches its own name, case-insensitively: motions are named
	// for the animations they drive.
	if got := classicOETreeJumpIndex(classicOETreeRow{Name: "runaim_d"}, actions, clips); got != 3 {
		t.Errorf("leaf jump = %d, want 3 (runaim_d)", got)
	}
	if got := classicOETreeJumpIndex(classicOETreeRow{Name: "MOVE"}, actions, clips); got != 2 {
		t.Errorf("leaf jump MOVE = %d, want 2 (move)", got)
	}

	// A branch whose own name is no clip dives into its subtree in the
	// order the tree draws the children: the subaction before the root's
	// own motion.
	row := classicOETreeRow{Name: "default", Key: "/default"}
	if got := classicOETreeJumpIndex(row, actions, clips); got != 1 {
		t.Errorf("default jump = %d, want 1 (idle_1 through idlefire)", got)
	}

	// A branch with no subactions falls through to its own motion leaf.
	row = classicOETreeRow{Name: "die", Key: "/die"}
	if got := classicOETreeJumpIndex(row, actions, clips); got != 0 {
		t.Errorf("die jump = %d, want 0 (die_1)", got)
	}

	// The Actions folder lists every action, not just roots: a non-root
	// still resolves through its own name.
	row = classicOETreeRow{Name: "idlefire", Key: "/idlefire"}
	if got := classicOETreeJumpIndex(row, actions, clips); got != 1 {
		t.Errorf("idlefire jump = %d, want 1 (idle_1)", got)
	}

	// Nothing in the subtree names a clip — the press reports a miss.
	row = classicOETreeRow{Name: "flag_motion", Key: "/flag_motion"}
	if got := classicOETreeJumpIndex(row, actions, clips); got != -1 {
		t.Errorf("flag_motion jump = %d, want -1", got)
	}
	if got := classicOETreeJumpIndex(classicOETreeRow{Name: "nope"}, actions, clips); got != -1 {
		t.Errorf("leaf miss = %d, want -1", got)
	}

	// A self-referential action terminates and reports no clip.
	row = classicOETreeRow{Name: "loop", Key: "/loop"}
	if got := classicOETreeJumpIndex(row, actions, clips); got != -1 {
		t.Errorf("loop jump = %d, want -1 (cycle, no clip)", got)
	}
}

func TestClassicOEDoubleClick(t *testing.T) {
	var ui uiState
	if ui.classicOEDoubleClick("motion:phase_in") {
		t.Fatal("the first press reported a double")
	}
	if !ui.classicOEDoubleClick("motion:phase_in") {
		t.Fatal("an immediate second press on the same row was not a double")
	}
	if ui.classicOEDoubleClick("motion:phase_in") {
		t.Fatal("a third press repeated the double instead of starting fresh")
	}
	if ui.classicOEDoubleClick("action:default") {
		t.Fatal("a first press on another row counted as a double")
	}
}
