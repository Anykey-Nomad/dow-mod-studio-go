package unitdef

import (
	"path"
	"sort"
	"strings"

	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/rgd"
)

// UnitVoiceRoots are the folders a unit's replies live under, in the order the
// game looks for them: the speech tree most mods use, and the older one that
// spells the same races/<race>/<unit> path without it. Both sit under
// Data\sound, and the unit's own folder is a category per action inside them.
var UnitVoiceRoots = []string{`Data\sound\speech\races`, `Data\sound\races`}

// UnitSpeechDirectory is the value of ui_ext/speech_directory: the folder the
// unit's replies are voiced in, written the way the game's data spells it
// ("Speech/Races/Necron/Builder_Scarab"). It is empty when the blueprint
// declares no voice of its own, which most buildings and some units do not.
func UnitSpeechDirectory(rf *rgd.RgdFile) string {
	if rf == nil || rf.Root == nil {
		return ""
	}
	e := RgdFindPath(rf.Root, "ui_ext", "speech_directory")
	if e == nil {
		return ""
	}
	return strings.TrimSpace(e.DisplayFieldValue())
}

// UnitVoiceSuffix turns a speech_directory into the part under the race tree:
// "Speech/Races/Necron/Builder_Scarab" gives "necron\builder_scarab". A value
// that does not point into a speech tree gives "" — the unit has no voice to
// resolve, and an empty suffix must not turn into a search of the whole race
// tree, which would list every unit's replies as if they were this one's.
func UnitVoiceSuffix(speech string) string {
	s := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(speech), "/", `\`))
	rest, found := strings.CutPrefix(s, `speech\races\`)
	if !found {
		return ""
	}
	return strings.Trim(rest, `\`)
}

// UnitVoiceFolderPath is where a unit's replies would be: root joined with the
// suffix, backslash separated, e.g.
// Data\sound\speech\races\necron\builder_scarab.
func UnitVoiceFolderPath(root, suffix string) string {
	root, suffix = strings.Trim(root, `\`), strings.Trim(suffix, `\`)
	if root == "" || suffix == "" {
		return root
	}
	return root + `\` + suffix
}

// UnitVoiceFile is one reply file in a category folder: the name the folder
// gives it, its path in the mod tree, and the format it is in. A folder holds
// .fda takes, which is what the game plays, and a .wav beside them is either a
// take someone exported to edit or one about to be encoded back.
type UnitVoiceFile struct {
	Name string
	Path string
	// Ext is the lower-case extension with its dot: ".fda" or ".wav".
	Ext string
}

// UnitVoiceCategory is one action's folder of replies — "move", "die",
// "capture_strategic_point" — with the takes inside it. The names are the
// action names the .whe events and the order UI use, so a category says which
// moments of the unit are voiced.
type UnitVoiceCategory struct {
	Name   string
	Folder string
	Files  []UnitVoiceFile
}

// UnitVoiceSet is what a unit's speech_directory resolves to in the mod.
type UnitVoiceSet struct {
	// Speech is ui_ext/speech_directory as the file writes it.
	Speech string
	// Suffix is the part under the race tree, empty when the value points at
	// no speech tree at all.
	Suffix string
	// Root is the folder the replies were found under, empty when they were
	// found under none.
	Root string
	// Searched is every folder that was tried, in order, so a column can say
	// where it looked instead of only that it found nothing.
	Searched []string
	// Categories are the folders holding .fda files, ordered by name.
	Categories []UnitVoiceCategory
}

// VoiceCount is how many replies the set holds in total, which is what a
// summary line reports.
func (s UnitVoiceSet) VoiceCount() int {
	n := 0
	for _, c := range s.Categories {
		n += len(c.Files)
	}
	return n
}

// UnitVoices resolves a unit's speech folder in the mod's tree. It reads the
// tree's folder list rather than the disk: the files are already indexed, so
// this is cheap enough to run while laying the section out.
func UnitVoices(tree *modtree.ModTree, rf *rgd.RgdFile) UnitVoiceSet {
	set := UnitVoiceSet{Speech: UnitSpeechDirectory(rf)}
	if tree == nil || tree.Root == nil {
		return set
	}
	set.Suffix = UnitVoiceSuffix(set.Speech)
	if set.Suffix == "" {
		return set
	}
	for _, root := range UnitVoiceRoots {
		folder := UnitVoiceFolderPath(root, set.Suffix)
		set.Searched = append(set.Searched, folder)
		f := unitVoiceFolder(tree.Root, folder)
		if f == nil {
			continue
		}
		set.Root = folder
		set.Categories = unitVoiceCategories(f)
		// The first root that has the folder wins, the same way the game
		// stops at the first tree it finds the unit in: a mod that keeps
		// both would otherwise list each reply twice.
		break
	}
	return set
}

// unitVoiceFolder resolves a virtual folder path under the tree root. The root
// of the tree is Data itself, so a path that names it is taken as already
// inside rather than looked for as a child.
func unitVoiceFolder(root *modtree.TreeFolder, virtualPath string) *modtree.TreeFolder {
	segs := strings.Split(strings.Trim(virtualPath, `\`), `\`)
	if len(segs) > 0 && strings.EqualFold(segs[0], root.Name) {
		segs = segs[1:]
	}
	cur := root
	for _, seg := range segs {
		if seg == "" {
			continue
		}
		cur = modtree.FindFolderByLower(cur, seg)
		if cur == nil {
			return nil
		}
	}
	return cur
}

// unitVoiceCategories reads a unit's folder into categories: one per child
// folder that holds .fda files. A folder that keeps its replies without the
// per-action split is reported as one category named after the unit's own
// folder, so those files are still reachable rather than looking like no voice
// at all.
func unitVoiceCategories(folder *modtree.TreeFolder) []UnitVoiceCategory {
	cats := make([]UnitVoiceCategory, 0, len(folder.Children))
	for _, sub := range folder.Children {
		files := unitVoiceFiles(sub.Files)
		if len(files) == 0 {
			continue
		}
		cats = append(cats, UnitVoiceCategory{Name: sub.Name, Folder: sub.FullPath, Files: files})
	}
	if len(cats) == 0 {
		if files := unitVoiceFiles(folder.Files); len(files) > 0 {
			cats = append(cats, UnitVoiceCategory{Name: folder.Name, Folder: folder.FullPath, Files: files})
		}
	}
	sort.Slice(cats, func(i, j int) bool {
		return strings.ToLower(cats[i].Name) < strings.ToLower(cats[j].Name)
	})
	return cats
}

// unitVoiceFiles picks the reply files of a folder and sorts them, so a category
// lists its takes in a stable order rather than in whatever order the tree was
// built. The formats are the two the section can play and convert between.
func unitVoiceFiles(files []*modtree.TreeFile) []UnitVoiceFile {
	var out []UnitVoiceFile
	for _, f := range files {
		ext := strings.ToLower(path.Ext(f.Name))
		if ext != ".fda" && ext != ".wav" {
			continue
		}
		out = append(out, UnitVoiceFile{Name: f.Name, Path: f.FullPath, Ext: ext})
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}
