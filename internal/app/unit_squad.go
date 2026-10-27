package app

import (
	"dow-mod-studio/internal/app/uikit"
	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/rgd"
	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget/material"
	"image"
	"strings"
)

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

// layoutUnitSquadRow is one squad: its name, and the size the squad is fielded
// at. The size is shown on the row because it is the answer to the question a
// reader opens this column with — a necron lord is 1, a necron warrior is 3 to 8
// — and finding it means opening the squad otherwise.
func LayoutUnitSquadRow(ui *uiState, gtx layout.Context, index int, s unitSquadRef) layout.Dimensions {
	sel := index == ui.UnitSelRow
	bg := uikit.CPanelBg
	switch {
	case sel:
		bg = uikit.CSelectBg
	case ui.UnitRowClicks[index].Hovered():
		bg = uikit.CMenuHover
	}
	return ui.UnitRowClicks[index].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		h := gtx.Dp(unit.Dp(squadRowHeightDp))
		sz := image.Pt(gtx.Constraints.Max.X, h)
		gtx.Constraints.Min = sz
		gtx.Constraints.Max = sz
		return layout.Stack{}.Layout(gtx,
			layout.Expanded(func(gtx layout.Context) layout.Dimensions {
				if bg != uikit.CPanelBg {
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
							l.Color = uikit.CText
							l.MaxLines = 1
							return l.Layout(gtx)
						}),
						layout.Flexed(1, flexGap()),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.Th, unit.Sp(9), squadSizeLabel(s))
							l.Color = uikit.CTextDim
							l.MaxLines = 1
							return l.Layout(gtx)
						}),
					)
				})
			}),
		)
	})
}

// layoutUnitSquadRightColumn is the right column: the selected squad's own
// properties, read out of its .rgd. This is the part of the squad the unit's own
// file cannot say anything about — its stance, its morale, its cap cost, how it
// captures — none of which belongs to an individual trooper.
func LayoutUnitSquadRightColumn(ui *uiState, gtx layout.Context, sec unitSection) layout.Dimensions {
	d := CurrentUnitDetails(ui)
	if d == nil || d.Error != "" {
		return UnitColumn(ui, gtx, sec.RightTitle, "No entry is selected.")
	}
	squads := SquadsOfUnitOf(ui, d.Entry)
	if len(squads) == 0 {
		return UnitColumn(ui, gtx, sec.RightTitle, "No entry is selected.")
	}
	idx := clampInt(ui.UnitSelRow, 0, len(squads)-1)
	s := squads[idx]
	fields := squadFields(ui.ModTree, s)
	return panelFrame(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return LayoutUnitColumnHeader(ui, gtx, sec.RightTitle)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, uikit.CHeaderLine, 1) }),
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
	if CurrentUnitSection(ui).Label != "Squads" {
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
		add(sc.Group, sc.Label, rgdEntryStr(rgdFindPath(rf.Root, sc.Path...)))
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
