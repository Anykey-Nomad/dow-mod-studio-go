package unitdef

import (
	"strings"

	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/rgd"
	"dow-mod-studio/internal/whm"
)

// UnitModelPath is the .whm a unit draws with. entity_blueprint_ext\animator
// names the model as "Races\<Race>\Troops\<Model>" with no extension, and the
// file itself sits under Data\art\ebps with the same folders — which is where a
// weapon's meshes live too, since a weapon is a mesh group of the unit's model
// and not a model of its own. "" when the unit names no model or the mod holds
// none by that name.
func UnitModelPath(tree *modtree.ModTree, rf *rgd.RgdFile) string {
	if rf == nil || rf.Root == nil {
		return ""
	}
	anim := strings.TrimSpace(RgdEntryStr(RgdFindPath(rf.Root, "entity_blueprint_ext", "animator")))
	if anim == "" || tree == nil {
		return ""
	}
	p := strings.ReplaceAll(anim, "/", `\`)
	p = strings.TrimSuffix(strings.TrimSuffix(p, `.lua`), `.whm`)
	if f := tree.FindFile(`Data\art\ebps\` + p + `.whm`); f != nil {
		return f.FullPath
	}
	base := p
	if i := strings.LastIndex(base, `\`); i >= 0 {
		base = base[i+1:]
	}
	if f := tree.FindByName(base + `.whm`); f != nil {
		return f.FullPath
	}
	return ""
}

// ModelMaterialWarning is one mesh whose material the module cannot supply.
type ModelMaterialWarning struct {
	// Mesh is the mesh's name as the model spells it.
	Mesh string
	// Material is the reference the model carries, which is what cannot be
	// resolved.
	Material string
	// Groups are the visibility groups the mesh belongs to. A weapon choice
	// switches such a group on, which is why a missing material there only
	// shows once that weapon is mounted.
	Groups []string
}

// UnitWeaponModelWarnings reports the meshes of a unit's model that carry a
// material reference the module cannot resolve, with the visibility groups that
// show them.
//
// This is the check that catches a model which renders as the game's placeholder
// cube the moment a weapon upgrade mounts: the model parses, the mesh is there,
// and only its texture reference is wrong — often the exporter's default
// material, left as the bare word "Material" under a path that holds nothing.
// Nothing in the .rgd says a word about it, so the file looks correct until the
// group becomes visible in game.
func UnitWeaponModelWarnings(tree *modtree.ModTree, rf *rgd.RgdFile) []ModelMaterialWarning {
	path := UnitModelPath(tree, rf)
	if path == "" || tree == nil {
		return nil
	}
	file := tree.FindFile(path)
	if file == nil {
		return nil
	}
	data, err := tree.ReadTreeFile(file)
	if err != nil {
		return nil
	}
	m, err := whm.Parse(data)
	if err != nil {
		return nil
	}
	// A model names a mesh one way and a group track names it another — casing,
	// spaces and underscores all differ between them — so both sides are keyed by
	// a name with everything but letters and digits dropped.
	label := make(map[string]string, len(m.Meshes))
	materials := make(map[string]string, len(m.Meshes))
	var order []string
	for _, mesh := range m.Meshes {
		name := strings.TrimSpace(mesh.Name)
		key := normName(name)
		if name == "" || key == "" {
			continue
		}
		if _, seen := materials[key]; seen {
			continue
		}
		ref := strings.TrimSpace(mesh.MaterialPath)
		if ref == "" {
			ref = strings.TrimSpace(mesh.Material)
		}
		if modelMaterialFound(tree, path, ref) {
			continue
		}
		label[key] = name
		materials[key] = ref
		order = append(order, key)
	}
	if len(order) == 0 {
		return nil
	}
	// Every animation clip lists the meshes it plays, so a plain clip names these
	// meshes too and would bury the group that matters. Groups are therefore
	// ranked per mesh: one whose name shares a word with the mesh first (a
	// "Volkite Cannon" is switched by vis_volkite), then the choice groups a
	// weapon or an upgrade toggles, and only then a plain clip as a fallback.
	byMesh := make(map[string][]string, len(materials))
	for _, anim := range m.Animations {
		for _, track := range anim.Meshes {
			key := normName(track.Name)
			if _, ok := materials[key]; !ok {
				continue
			}
			if !containsFold(byMesh[key], anim.Name) {
				byMesh[key] = append(byMesh[key], anim.Name)
			}
		}
	}
	groups := make(map[string][]string, len(order))
	limits := [4]int{3, 3, 1, 1}
	for _, key := range order {
		for level := 0; level < len(limits); level++ {
			for _, group := range byMesh[key] {
				if groupPriority(group, label[key]) != level || len(groups[key]) >= limits[level] {
					continue
				}
				if !containsFold(groups[key], group) {
					groups[key] = append(groups[key], group)
				}
			}
			if len(groups[key]) > 0 {
				break
			}
		}
	}
	out := make([]ModelMaterialWarning, 0, len(order))
	for _, key := range order {
		out = append(out, ModelMaterialWarning{Mesh: label[key], Material: materials[key], Groups: groups[key]})
	}
	return out
}

// modelMaterialFound reports whether any archive the module loads holds the
// material a model reference names.
//
// A .whm states a material as a path under art, with or without an extension,
// and the shipped files are .rsh, .rtx and .wtp. The declared path is tried
// with each of those, then beside the model, which is where an exporter puts a
// texture it shipped with the file. Only a reference that carries no directory
// at all is looked up by bare name: a path that names a directory is not rescued
// by a file of the same name somewhere else — and the game does not rescue it
// either, which is exactly how a broken "texture_share/Material" reference still
// looked resolved to a search that matched on the last path element.
func modelMaterialFound(tree *modtree.ModTree, modelPath, material string) bool {
	if tree == nil {
		return true
	}
	name := strings.TrimSpace(material)
	if name == "" {
		return false
	}
	p := strings.ReplaceAll(name, "/", `\`)
	if len(p) >= 5 && strings.EqualFold(p[:5], `data\`) {
		p = p[5:]
	}
	p = strings.TrimLeft(p, `\`)
	if p == "" {
		return false
	}
	base, dir := p, ""
	if i := strings.LastIndex(p, `\`); i >= 0 {
		base, dir = p[i+1:], p[:i]
	}
	stem := base
	if i := strings.LastIndex(stem, "."); i > 0 {
		stem = stem[:i]
	}
	exts := []string{".rsh", ".rtx", ".wtp", ".dds"}
	for _, ext := range exts {
		if tree.FindFile(`Data\`+p+ext) != nil {
			return true
		}
	}
	if strings.Contains(base, ".") && tree.FindFile(`Data\`+p) != nil {
		return true
	}
	if dir := modelDir(modelPath); dir != "" && stem != "" {
		for _, ext := range exts {
			if tree.FindFile(`Data\`+dir+`\`+stem+ext) != nil {
				return true
			}
		}
	}
	if dir != "" {
		return false
	}
	for _, ext := range exts {
		if tree.FindMaterialByName(stem+ext) != nil {
			return true
		}
	}
	return false
}

// modelDir is the folder a model sits in, relative to Data, so a material can be
// looked for beside it.
func modelDir(modelPath string) string {
	p := strings.ReplaceAll(modelPath, "/", `\`)
	if len(p) >= 5 && strings.EqualFold(p[:5], `data\`) {
		p = p[5:]
	}
	if i := strings.LastIndex(p, `\`); i >= 0 {
		return p[:i]
	}
	return ""
}

// normName lower cases a name and keeps only its letters and digits, so a mesh
// and a group track that spell the same thing differently still meet.
func normName(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// containsFold reports whether list holds name, ignoring case.
func containsFold(list []string, name string) bool {
	for _, item := range list {
		if strings.EqualFold(item, name) {
			return true
		}
	}
	return false
}

// isChoiceGroup reports whether an animation is a group a weapon choice or an
// upgrade switches on, rather than one the model plays.
func isChoiceGroup(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	return strings.HasPrefix(n, "vis_") || strings.HasPrefix(n, "ref_")
}

// groupPriority ranks a group for one mesh: 0 when the group's name shares a
// word with the mesh, 1 when it is a choice group, 2 for a clip the model plays.
func groupPriority(group, mesh string) int {
	switch choice, shared := isChoiceGroup(group), sharesWord(group, mesh); {
	case choice && shared:
		return 0
	case choice:
		return 1
	case shared:
		return 2
	}
	return 3
}

// sharesWord reports whether two names have a word of four letters or more in
// common, which is how "vis_volkite" and "Volkite Cannon" are recognised as the
// same thing without a table of naming rules.
func sharesWord(a, b string) bool {
	wordsB := words(b)
	for _, x := range words(a) {
		if len(x) < 4 {
			continue
		}
		for _, y := range wordsB {
			if x == y {
				return true
			}
		}
	}
	return false
}

// words splits a name into its lower cased parts at anything that is not a
// letter or a digit, so vis_volkite is vis and volkite.
func words(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9')
	})
}
