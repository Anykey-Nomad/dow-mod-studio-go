package unit_test

import (
	"dow-mod-studio/internal/app/unit"
	"testing"
)

// The action rows of the Classic Object Editor borrow the game's own command
// buttons as their icons: an action whose name says what it does ("move",
// "melee_charge", "capture_strategic_point") is matched to the tga the game
// would draw for that command. The mapping is pure string work on the action's
// name — it reads no file and no .whe — so it is pinned here against the names
// the real .whe files in the module actually carry.
//
// The icon names are the game's own files under
// Data\art\ui\ingame\command_icons, without the extension; a name the
// vocabulary does not cover maps to "" and the row keeps its text alone.

func TestActionIconFile(t *testing.T) {
	cases := []struct {
		action string
		want   string
	}{
		// The movement family.
		{"move", "move"},
		{"move_firing", "move"},
		{"move_tracking", "move"},
		{"idle_run", "move"},
		{"patrol", "move"},
		{"charge", "move"},
		{"sprint", "move"},
		// Melee comes before every attack and move rule, so however the
		// rest of the name reads it keeps the melee picture.
		{"melee", "melee"},
		{"melee_attack", "melee"},
		{"melee_charge", "melee"},
		{"move_melee", "melee"},
		// Attacks, most specific first.
		{"attack", "attackg"},
		{"attack_move", "attackm"},
		// Building.
		{"build", "build_structure"},
		{"build_advanced", "build_structure_advanced"},
		// Point control.
		{"capture_strategic_point", "capturepoint"},
		{"garrison_strategic_point", "capturepoint"},
		{"rally", "rallypoint"},
		{"repair", "repair"},
		{"stop", "stop"},
		// Transport and deployment.
		{"deploy_weapon", "deploy_weapon"},
		{"undeploy_weapon", "undeploy_weapon"},
		{"unload", "unloading"},
		{"unload_here", "unload_here"},
		{"attach", "attach"},
		{"detach", "detach"},
		// The more unusual commands.
		{"teleport", "teleport"},
		{"phase_in", "teleport"},
		{"jump", "jumping"},
		{"jump_setup", "jumping"},
		{"leap", "jumping"},
		{"entrench", "entrench"},
		{"cannibalize", "cannibalize"},
		{"possess", "possess"},
		{"scuttle", "scuttle"},
		{"reinforce", "reinforce"},
		{"uproot", "uproot"},
		{"deepstrike", "deepstrike"},
		{"droppod", "droppod"},
		{"summon", "summoning"},
		{"rampage", "rampage"},
		{"train_units", "train_units"},
		{"place_bomb", "place_bomb"},
		{"infiltrate", "infiltrate_on"},
		// Fire control, read before the generic stance family.
		{"holdfire", "stance_cease"},
		{"hold_fire", "stance_cease"},
		{"cease_fire", "stance_cease"},
		// The stance family, and the plain words that fall back to it.
		{"stance_hold", "stance_hold"},
		{"stance_fire", "stance_attack"},
		{"stance_ranged", "stance_ranged"},
		{"hold", "stance_hold"},
		{"idle", "stance_stand"},
		{"idle_firing", "stance_stand"},
		{"stand", "stance_stand"},
		// A name the vocabulary does not cover keeps no icon: these are
		// the states and cutscenes a .whe leans on that no command
		// button speaks for.
		{"default", ""},
		{"die", ""},
		{"die_thrown", ""},
		{"thrown", ""},
		{"thrown_getup", ""},
		{"syncdeath_1", ""},
		{"synckill_2", ""},
		{"camera_in", ""},
		{"vis_master", ""},
		{"race_enter", ""},
		{"sp_talk", ""},
		{"", ""},
		// A rule on a plain word must not catch it inside another:
		// "thrown" is not a run, and "holdfire" is not a hold.
		{"thrown_run", "move"},
	}
	for _, c := range cases {
		if got := unit.ActionIconFile(c.action); got != c.want {
			t.Errorf("actionIconFile(%q) = %q, want %q", c.action, got, c.want)
		}
	}
}

// A word-boundary rule catches the word as a word — alone, at either end, or
// between underscores — and nothing else.
func TestActionIconHasWord(t *testing.T) {
	cases := []struct {
		name, word string
		want       bool
	}{
		{"run", "run", true},
		{"run_in", "run", true},
		{"idle_run", "run", true},
		{"a_run_b", "run", true},
		{"running", "run", false},
		{"thrown", "run", false},
		{"rerun", "run", false},
	}
	for _, c := range cases {
		if got := unit.ActionIconHasWord(c.name, c.word); got != c.want {
			t.Errorf("actionIconHasWord(%q, %q) = %v, want %v", c.name, c.word, got, c.want)
		}
	}
}
