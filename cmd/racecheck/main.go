// Command racecheck is a scratch tool: it loads the race list, the
// localisation table and the player slots of a scenario through the public
// packages, so the data the setup screen needs is checked end to end.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"dow-mod-studio/modapi"
	"dow-mod-studio/racebps"
	"dow-mod-studio/sgbview"
	"dow-mod-studio/ucslocale"
)

const dowRoot = `C:\Program Files\Steam\steamapps\common\Dawn of War Definitive Edition`

func main() {
	root := flag.String("root", dowRoot, "путь к установленной Dawn of War")
	module := flag.String("module", "DoWDE", "модуль")
	scenario := flag.String("scenario", "", "сценарий .sgb")
	lang := flag.String("lang", "English", "язык локализации")
	flag.Parse()

	modules, err := modapi.DiscoverModules(*root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "discover:", err)
		os.Exit(1)
	}
	target := modapi.FindModule(modules, *module)
	if target == nil {
		fmt.Fprintln(os.Stderr, "модуль не найден:", *module)
		os.Exit(1)
	}
	tree, err := modapi.BuildModTree(target.Path, "English")
	if err != nil {
		fmt.Fprintln(os.Stderr, "build:", err)
		os.Exit(1)
	}
	defer tree.Close()

	table := ucslocale.Load(*root, "", *lang)
	fmt.Printf("локализация: язык %q, файлов %d, записей %d\n", table.Lang, table.Files, table.Lines)

	races, err := racebps.Load(tree, table)
	if err != nil {
		fmt.Fprintln(os.Stderr, "races:", err)
		os.Exit(1)
	}
	fmt.Printf("=== расы (%d) ===\n", len(races))
	for _, r := range races {
		fmt.Printf("  %-24s %-14s id=%-9s playable=%-5v постройки=%v\n",
			r.ID, r.Name, r.NameID, r.Playable, r.StartingBuildings)
		for _, b := range r.StartingBuildings {
			path := r.StructurePath(b)
			f := tree.FindFile(path)
			state := "нет"
			if f != nil {
				state = "есть"
			}
			fmt.Printf("      %-16s -> %s (%s)\n", b, path, state)
		}
	}

	if *scenario == "" {
		return
	}
	slots, err := sgbview.LoadPlayers(tree, *scenario)
	if err != nil {
		fmt.Fprintln(os.Stderr, "players:", err)
		os.Exit(1)
	}
	fmt.Printf("=== игроки %s (%d) ===\n", *scenario, len(slots))
	for _, s := range slots {
		fmt.Printf("  игрок %d: стартовых позиций %d\n", s.Owner, len(s.Positions))
		for _, p := range s.Positions {
			fmt.Printf("      %-60s x=%.1f y=%.1f z=%.1f\n",
				strings.TrimSpace(p.Name), p.X, p.Y, p.Z)
		}
	}

	// Стартовые постройки: первая играбельная раса каждому слоту.
	var builds []sgbview.SlotBuild
	for _, s := range slots {
		for _, r := range races {
			if !r.Playable || len(r.StartingBuildings) == 0 {
				continue
			}
			builds = append(builds, sgbview.SlotBuild{
				Slot:      s.Owner,
				Blueprint: r.StructurePath(r.StartingBuildings[0]),
			})
			break
		}
	}
	fmt.Println("--- сцена со стартовыми постройками ---")
	for _, b := range builds {
		fmt.Printf("  игрок %d -> %s\n", b.Slot, b.Blueprint)
	}
	sc, err := sgbview.LoadWith(tree, *scenario, sgbview.DefaultLimits(), builds)
	if err != nil {
		fmt.Fprintln(os.Stderr, "сцена:", err)
		os.Exit(1)
	}
	fmt.Printf("  объектов: %d, сущностей: %d, пропущено: %d\n",
		len(sc.Objects), sc.Entities, len(sc.Skipped))
	for _, s := range sc.Skipped {
		fmt.Printf("    %-70s %s\n", s.Name, s.Reason)
	}
}
