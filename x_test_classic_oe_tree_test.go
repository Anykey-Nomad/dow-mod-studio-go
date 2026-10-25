package main

import (
	"bytes"
	"strconv"
	"testing"

	"dow-mod-studio/internal/chunky"
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

// A double press on a tree row opens that row's own editor: an action row
// raises the action's condition editor, a motion leaf the timeline of the
// same name — the same two dialogs the Actions and Motions folders raise.
// A row whose name is neither has no editor, so no dialog is named for it.
func TestClassicOETreeRowDialogPicksTheEditorsArea(t *testing.T) {
	mton := func(name string) *chunky.Chunk {
		payload := (&whe.Mton{Animations: []string{name + "_clip"}}).MarshalBinary()
		return &chunky.Chunk{TypeID: "DATAMTON", Version: 4, Name: name, Payload: payload}
	}
	mt := &chunky.Chunk{TypeID: "FOLDMTRE", Version: 1}
	mt.Children = append(mt.Children, mton("idle_shared"))
	acts := []whe.FxAction{{Name: "idle", Motions: []whe.FxActionLink{
		{Name: "idle_shared", Compare: whe.FxCompareNone},
	}}}
	root := &chunky.Chunk{TypeID: "FOLDREBP", Version: 4, Children: []*chunky.Chunk{
		{TypeID: "DATAACTS", Version: 1, Payload: whe.MarshalActs(acts)},
		mt,
	}}
	var buf bytes.Buffer
	buf.Write(chunky.Preamble())
	buf.Write(chunky.Serialize(root))
	doc, err := whe.ParseDocument(buf.Bytes())
	if err != nil {
		t.Fatalf("parsing the fixture .whe: %v", err)
	}

	kind, name, index, ok := classicOETreeRowDialog(doc, classicOETreeRow{Name: "idle", Key: "/idle"})
	if !ok || kind != classicOEDialogAction || name != "idle" || index != 0 {
		t.Errorf("action row = (%d, %q, %d, %v), want the action editor at index 0", kind, name, index, ok)
	}

	kind, name, _, ok = classicOETreeRowDialog(doc, classicOETreeRow{Name: "idle_shared"})
	if !ok || kind != classicOEDialogTimeline || name != "idle_shared" {
		t.Errorf("motion leaf = (%d, %q, _, %v), want the timeline editor for idle_shared", kind, name, ok)
	}

	// A motion the file carries no timeline for has no editor to raise.
	if _, _, _, ok := classicOETreeRowDialog(doc, classicOETreeRow{Name: "missing_clip"}); ok {
		t.Error("a name with no timeline and no action reported an editor")
	}
	// Nor does a branch whose action is not in the file (a broken tree).
	if _, _, _, ok := classicOETreeRowDialog(doc, classicOETreeRow{Name: "gone", Key: "/gone"}); ok {
		t.Error("an action row with no such action reported an editor")
	}
	if _, _, _, ok := classicOETreeRowDialog(nil, classicOETreeRow{Name: "idle", Key: "/idle"}); ok {
		t.Error("a nil document reported an editor")
	}
}

// TestClassicOEHeadersKeepTheAnimsPanelDefault guards the panel list: the
// anims panel must stay the one that starts open even after another panel —
// the mesh list — is appended past it, which is why the index is found by
// kind rather than by being last.
func TestClassicOEHeadersKeepTheAnimsPanelDefault(t *testing.T) {
	if classicOEHeaders[classicOEAnimsHeader].kind != classicOEKindAnims {
		t.Fatalf("default-open panel = %q (%s), want the anims panel",
			classicOEHeaders[classicOEAnimsHeader].label,
			classicOEHeaders[classicOEAnimsHeader].kind)
	}
	meshes := false
	for _, h := range classicOEHeaders {
		if h.kind == classicOEKindMeshes {
			meshes = true
		}
	}
	if !meshes {
		t.Error("the panel list carries no Meshes panel")
	}
}
