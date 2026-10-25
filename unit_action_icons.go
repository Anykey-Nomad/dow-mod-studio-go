package main

import (
	"image"
	"strings"
)

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
func (ui *uiState) actionIcon(name string) image.Image {
	file := actionIconFile(name)
	if file == "" || ui.modTree == nil {
		return nil
	}
	mod := ui.modTree.FileMapName
	if ui.actionIcons == nil || ui.actionIconsMod != mod {
		ui.actionIcons = make(map[string]iconCacheEntry)
		ui.actionIconsMod = mod
	}
	key := strings.ToLower(strings.TrimSpace(name))
	if e, ok := ui.actionIcons[key]; ok {
		return e.img
	}
	img := ui.readIconAt(actionIconCommandsDir + `\` + file)
	ui.actionIcons[key] = iconCacheEntry{img: img, tried: true}
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
