package main

import (
	"sort"
	"strings"

	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget/material"
)

// The relationships view.
//
// What the Weapons and Abilities lists show is half a picture: they say what a
// unit uses, and nothing about what uses it. That second half is the one a mod
// author needs. A weapon they are about to change is named by four hundred
// units. A unit they have just written is named by nothing. A building they
// froze still has a squad file and three other units naming it, and those are
// the things that will break.
//
// So the list is both directions, and it says which is which. The rows above
// the divider are what this unit refers to; the rows below are what refers to
// it. Freezing reads the second list and nothing else.

// linkRow is one relationship, as a row.
type linkRow struct {
	// role is what the relationship means, e.g. "weapon" or "built by".
	role string
	// slot is the numbered slot it sits in, e.g. "ability_02".
	slot string
	// target is the name of the other end, e.g. "chaos_bolt_pistol".
	target string
	// path is the other end's virtual path, empty when it resolves to
	// nothing.
	path string
	// found reports whether the reference resolved. A row that did not is
	// the one thing in this window that is certainly wrong.
	found bool
	// outgoing says which direction this row reads: out of this unit, or
	// into it.
	outgoing bool
}

// linkRows is the whole relationship list of one unit, in the order the list
// shows it: the roles the game itself groups by, in file order within each,
// and then the reverse links the same way.
func linkRows(g *modGraph, path string) []linkRow {
	if g == nil || path == "" {
		return nil
	}
	var out []linkRow
	for _, l := range g.Outgoing(path) {
		out = append(out, linkRow{
			role: l.Role.Label(), slot: l.Slot, target: l.TargetName(),
			path: l.To, found: l.Resolved(), outgoing: true,
		})
	}
	for _, l := range g.Incoming(path) {
		// A unit that refers to itself is its own production chain, and
		// saying so twice helps nobody.
		if strings.EqualFold(l.From, path) {
			continue
		}
		out = append(out, linkRow{
			role: l.Role.Label(), slot: l.Slot, target: graphEntityName(g, l.From),
			path: l.From, found: l.Resolved(), outgoing: false,
		})
	}
	return out
}

// graphEntityName is an entity's own name for itself, falling back to its file
// name when the graph does not know it — which happens for a path the
// inventory missed, and is worth showing rather than hiding.
func graphEntityName(g *modGraph, path string) string {
	if e := g.Entity(path); e != nil {
		return e.Name
	}
	return refStem(path)
}

// groupLinkRows splits a list where one direction ends and the other begins,
// which is the one divider in this view.
func groupLinkRows(rows []linkRow) (out []linkRow, in []linkRow) {
	for _, r := range rows {
		if r.outgoing {
			out = append(out, r)
		} else {
			in = append(in, r)
		}
	}
	return out, in
}

// layoutUnitLinksList is the "Links" column.
func (ui *uiState) layoutUnitLinksList(gtx layout.Context) layout.Dimensions {
	sel := ui.currentUnit()
	if sel == nil {
		return ui.unitSectionFrame(gtx, ui.unitEmptyState)
	}
	g := ui.startGraphLinkScan()
	if g == nil {
		return ui.unitSectionFrame(gtx, ui.unitEmptyState)
	}
	outgoing, incoming := groupLinkRows(linkRows(g, sel.Path))
	if len(outgoing) == 0 && len(incoming) == 0 {
		return ui.unitSectionFrame(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(12), Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return ui.linkNote(gtx, ui.linkScanNote())
			})
		})
	}
	return ui.unitSectionFrame(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(6), Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			var children []layout.FlexChild
			add := func(title string, rows []linkRow) {
				if len(rows) == 0 {
					return
				}
				children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return ui.linkGroupTitle(gtx, title, len(rows))
				}))
				for _, r := range rows {
					r := r
					children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return ui.linkRow(gtx, r)
					}))
				}
			}
			add("This unit refers to", outgoing)
			add("Refers to this unit", incoming)
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.linkNote(gtx, ui.linkScanNote())
			}))
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
		})
	})
}

// linkGroupTitle is the heading of one direction of the list.
func (ui *uiState) linkGroupTitle(gtx layout.Context, title string, n int) layout.Dimensions {
	return layout.Inset{Top: unit.Dp(6), Left: unit.Dp(4), Bottom: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		l := material.Label(ui.th, unit.Sp(9), title+"  ("+itoa(n)+")")
		l.Color = cTextDim
		return l.Layout(gtx)
	})
}

// linkRow is one relationship line: what it is, where it sits, and what it
// names. A reference that resolved to nothing is said so, because that is the
// line the reader is looking for.
func (ui *uiState) linkRow(gtx layout.Context, r linkRow) layout.Dimensions {
	name := r.target
	if !r.found {
		name = r.target + "  (not found)"
	}
	label := r.role
	if r.slot != "" {
		label = r.role + "  " + r.slot
	}
	return layout.Inset{Left: unit.Dp(4), Top: unit.Dp(1), Bottom: unit.Dp(1)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					w := gtx.Dp(unit.Dp(150))
					gtx.Constraints.Min.X = w
					gtx.Constraints.Max.X = w
					l := material.Label(ui.th, unit.Sp(10), clipName(label, 26))
					l.Color = cTextDim
					return l.Layout(gtx)
				})
			}),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				l := material.Label(ui.th, unit.Sp(10), name)
				if r.found {
					l.Color = cText
				} else {
					l.Color = cProblem
				}
				return l.Layout(gtx)
			}),
		)
	})
}

// linkNote is the small line at the foot of the list that says whether the mod
// has been read all the way through yet.
func (ui *uiState) linkNote(gtx layout.Context, text string) layout.Dimensions {
	return layout.Inset{Top: unit.Dp(8), Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		l := material.Label(ui.th, unit.Sp(9), text)
		l.Color = cTextDim
		return l.Layout(gtx)
	})
}

// linkScanNote says how much of the mod the background read has covered, so a
// list that is still filling up does not look like a list that is finished.
//
// It counts the files that are actually read, not the units: the references
// live in abilities, add-ons, weapons, squads and research as much as in the
// units themselves, and a note that said "604 units" would be leaving out most
// of what it had just looked at.
func (ui *uiState) linkScanNote() string {
	g := ui.graphFor()
	if g == nil {
		return "No mod is loaded."
	}
	files := len(g.EntitiesOfKind(graphLinkKinds()...))
	if ui.graphLinksRunning {
		return "Reading the mod… " + itoa(len(g.Links)) + " references so far."
	}
	if files == 0 {
		return "The mod has no units, buildings, abilities, add-ons, weapons, squads or research."
	}
	return itoa(len(g.Links)) + " references read from " + itoa(files) + " files."
}

// ── the mod-wide report ─────────────────────────────────────────────────────

// brokenLinkGroup is one role's worth of broken references, which is how the
// report reads: a weapon that went missing breaks every unit that carried it,
// and the shape of that is "twelve units, one missing file", not twelve
// separate facts.
type brokenLinkGroup struct {
	// ref is the reference as written, e.g. "weapon\necron_gauss.lua".
	ref string
	// role is what it was a reference to.
	role graphRole
	// holders are the files that held it.
	holders []string
}

// brokenLinkGroups collects the broken references of a mod into one entry per
// distinct reference, ordered by how many files are left pointing at nothing.
func brokenLinkGroups(g *modGraph) []brokenLinkGroup {
	if g == nil {
		return nil
	}
	type key struct {
		ref  string
		role graphRole
	}
	groups := map[key]*brokenLinkGroup{}
	for _, l := range g.BrokenLinks() {
		k := key{ref: strings.ToLower(l.Ref), role: l.Role}
		grp := groups[k]
		if grp == nil {
			grp = &brokenLinkGroup{ref: l.Ref, role: l.Role}
			groups[k] = grp
		}
		grp.holders = append(grp.holders, l.From)
	}
	out := make([]brokenLinkGroup, 0, len(groups))
	for _, grp := range groups {
		sort.Strings(grp.holders)
		out = append(out, *grp)
	}
	sort.Slice(out, func(i, j int) bool {
		if len(out[i].holders) != len(out[j].holders) {
			return len(out[i].holders) > len(out[j].holders)
		}
		return out[i].ref < out[j].ref
	})
	return out
}

// layoutUnitProblemsList is the "Problems" column: every reference in the mod
// that names a file which is not there, one row per missing file rather than
// per occurrence.
func (ui *uiState) layoutUnitProblemsList(gtx layout.Context) layout.Dimensions {
	g := ui.startGraphLinkScan()
	if g == nil {
		return ui.unitSectionFrame(gtx, ui.unitEmptyState)
	}
	groups := brokenLinkGroups(g)
	if len(groups) == 0 {
		return ui.unitSectionFrame(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(12), Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				msg := "Every reference in the mod resolves."
				if ui.graphLinksRunning {
					msg = "Reading the mod…"
				}
				return ui.linkNote(gtx, msg)
			})
		})
	}
	return ui.unitSectionFrame(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(6), Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			var children []layout.FlexChild
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.linkNote(gtx, itoa(len(groups))+" references in the mod name a file that is not there.")
			}))
			for _, grp := range groups {
				grp := grp
				children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return ui.problemRow(gtx, grp)
				}))
			}
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
		})
	})
}

// problemRow is one missing file and the files that still point at it.
func (ui *uiState) problemRow(gtx layout.Context, grp brokenLinkGroup) layout.Dimensions {
	holders := itoa(len(grp.holders))
	if len(grp.holders) == 1 {
		holders = "1 file"
	} else {
		holders += " files"
	}
	return layout.Inset{Left: unit.Dp(4), Top: unit.Dp(2), Bottom: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					w := gtx.Dp(unit.Dp(150))
					gtx.Constraints.Min.X = w
					gtx.Constraints.Max.X = w
					l := material.Label(ui.th, unit.Sp(10), grp.role.Label())
					l.Color = cTextDim
					return l.Layout(gtx)
				})
			}),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						l := material.Label(ui.th, unit.Sp(10), grp.ref)
						l.Color = cProblem
						return l.Layout(gtx)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return ui.linkNote(gtx, "still named by "+holders+": "+clipName(holdersFirst(grp.holders), 48))
					}),
				)
			}),
		)
	})
}

// holdersFirst is the first of the files pointing at a missing reference, which
// is the one worth naming; the rest are behind the count.
func holdersFirst(holders []string) string {
	if len(holders) == 0 {
		return ""
	}
	return refStem(holders[0])
}

// clipName shortens a name to fit a column, with an ellipsis where it was cut.
// A file name that is too long to show is still a name, and a truncated one
// with no mark reads as a different one.
func clipName(s string, n int) string {
	if n <= 1 || len([]rune(s)) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}

// ─── merged from graph_scan.go ───────────────────────────────────────────────

// Reading the links in the background.
//
// The graph indexes every .rgd of Data\attrib from the folder layout alone,
// which is cheap. Finding the references inside the three thousand-odd
// referring files is not: measured at around thirty seconds for the DXP3 tree.
// Paying that on the frame the user opens the relationships view would freeze
// the window for as long as it took, so it is done the way the unit names and
// the squad index are done — off the UI thread, in the background, with
// whatever has arrived so far shown while the rest is still coming.
//
// The scan is started when the relationships view is opened rather than when
// the module is, because a session that never looks at them should not pay for
// them at all.

// graphLinkBatch is a slice of the scan's results. done marks the last one.
type graphLinkBatch struct {
	links []graphLink
	done  bool
}

// graphLinkBatchSize is how many links one batch carries. The links of one
// unit arrive together, so a unit is never split across two batches.
const graphLinkBatchSize = 128

// startGraphLinkScan reads every unit's and building's references in the
// background, into a fresh link list on the graph.
//
// A scan already running for the same tree is left alone, and a scan for a
// different tree is stopped rather than left writing into a graph nobody reads
// any more.
func (ui *uiState) startGraphLinkScan() *modGraph {
	g := ui.graphFor()
	if g == nil {
		return nil
	}
	if ui.graphLinksRunning && ui.graphLinksGraph == g {
		return g
	}
	ui.stopGraphLinkScan()

	ui.graphLinksGraph = g
	g.BeginLinkScan()
	ui.graphLinksCh = make(chan graphLinkBatch, 8)
	stop := make(chan struct{})
	ui.graphLinksStop = stop
	ui.graphLinksRunning = true

	// The entities are copied so the goroutine does not walk a graph the UI
	// thread is free to replace.
	entities := append([]*graphEntity(nil), g.EntitiesOfKind(graphLinkKinds()...)...)
	ch := ui.graphLinksCh
	go func() {
		batch := make([]graphLink, 0, graphLinkBatchSize)
		flush := func(done bool) bool {
			out := make([]graphLink, len(batch))
			copy(out, batch)
			select {
			case ch <- graphLinkBatch{links: out, done: done}:
				batch = batch[:0]
				return true
			case <-stop:
				return false
			}
		}
		for _, e := range entities {
			for _, l := range g.ScanEntityLinks(e) {
				batch = append(batch, l)
			}
			if len(batch) >= graphLinkBatchSize && !flush(false) {
				return
			}
		}
		flush(true)
	}()
	return g
}

// stopGraphLinkScan ends a running scan. The goroutine notices through its
// stop channel and returns, so a module reload does not leave one reading a
// tree that is being closed underneath it.
func (ui *uiState) stopGraphLinkScan() {
	if ui.graphLinksStop != nil {
		close(ui.graphLinksStop)
		ui.graphLinksStop = nil
	}
	ui.graphLinksRunning = false
	ui.graphLinksCh = nil
}

// pollGraphLinks takes whatever the scan has finished and reports whether
// anything new arrived, so the caller can ask for a redraw.
func (ui *uiState) pollGraphLinks() bool {
	if ui.graphLinksCh == nil {
		return false
	}
	got := false
	for {
		select {
		case batch := <-ui.graphLinksCh:
			if ui.graphLinksGraph != nil {
				ui.graphLinksGraph.AddScannedLinks(batch.links)
			}
			if batch.done {
				if ui.graphLinksGraph != nil {
					ui.graphLinksGraph.FinishLinkScan()
				}
				ui.stopGraphLinkScan()
			}
			got = true
		default:
			return got
		}
	}
}
