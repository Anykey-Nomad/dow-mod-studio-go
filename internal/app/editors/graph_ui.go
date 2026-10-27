package editors

import (
	"dow-mod-studio/internal/app/uikit"
	"dow-mod-studio/internal/app/uistate"
	"dow-mod-studio/internal/modgraph"
	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget/material"
	"sort"
	"strings"
)

// LinkRowData is one relationship, as a row.
type LinkRowData struct {
	// Role is what the relationship means, e.g. "weapon" or "built by".
	Role string
	// Slot is the numbered slot it sits in, e.g. "ability_02".
	Slot string
	// Target is the name of the other end, e.g. "chaos_bolt_pistol".
	Target string
	// Path is the other end's virtual path, empty when it resolves to
	// nothing.
	Path string
	// Found reports whether the reference resolved. A row that did not is
	// the one thing in this window that is certainly wrong.
	Found bool
	// Outgoing says which direction this row reads: out of this unit, or
	// into it.
	Outgoing bool
}

// GroupLinkRows splits a list where one direction ends and the other begins,
// which is the one divider in this view.
func GroupLinkRows(rows []LinkRowData) (out []LinkRowData, in []LinkRowData) {
	for _, r := range rows {
		if r.Outgoing {
			out = append(out, r)
		} else {
			in = append(in, r)
		}
	}
	return out, in
}

// linkGroupTitle is the heading of one direction of the list.
func LinkGroupTitle(ui *uistate.State, gtx layout.Context, title string, n int) layout.Dimensions {
	return layout.Inset{Top: unit.Dp(6), Left: unit.Dp(4), Bottom: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		l := material.Label(ui.Th, unit.Sp(9), title+"  ("+Itoa(n)+")")
		l.Color = uikit.CTextDim
		return l.Layout(gtx)
	})
}

// LinkRow is one relationship line: what it is, where it sits, and what it
// names. A reference that resolved to nothing is said so, because that is the
// line the reader is looking for.
func LinkRow(ui *uistate.State, gtx layout.Context, r LinkRowData) layout.Dimensions {
	name := r.Target
	if !r.Found {
		name = r.Target + "  (not found)"
	}
	label := r.Role
	if r.Slot != "" {
		label = r.Role + "  " + r.Slot
	}
	return layout.Inset{Left: unit.Dp(4), Top: unit.Dp(1), Bottom: unit.Dp(1)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					w := gtx.Dp(unit.Dp(150))
					gtx.Constraints.Min.X = w
					gtx.Constraints.Max.X = w
					l := material.Label(ui.Th, unit.Sp(10), ClipName(label, 26))
					l.Color = uikit.CTextDim
					return l.Layout(gtx)
				})
			}),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				l := material.Label(ui.Th, unit.Sp(10), name)
				if r.Found {
					l.Color = uikit.CText
				} else {
					l.Color = uikit.CProblem
				}
				return l.Layout(gtx)
			}),
		)
	})
}

// linkNote is the small line at the foot of the list that says whether the mod
// has been read all the way through yet.
func LinkNote(ui *uistate.State, gtx layout.Context, text string) layout.Dimensions {
	return layout.Inset{Top: unit.Dp(8), Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		l := material.Label(ui.Th, unit.Sp(9), text)
		l.Color = uikit.CTextDim
		return l.Layout(gtx)
	})
}

// BrokenLinkGroup is one role's worth of broken references, which is how the
// report reads: a weapon that went missing breaks every unit that carried it,
// and the shape of that is "twelve units, one missing file", not twelve
// separate facts.
type BrokenLinkGroup struct {
	// Ref is the reference as written, e.g. "weapon\necron_gauss.lua".
	Ref string
	// Role is what it was a reference to.
	Role modgraph.GraphRole
	// Holders are the files that held it.
	Holders []string
}

// BrokenLinkGroups collects the broken references of a mod into one entry per
// distinct reference, ordered by how many files are left pointing at nothing.
func BrokenLinkGroups(g *uistate.ModGraph) []BrokenLinkGroup {
	if g == nil {
		return nil
	}
	type key struct {
		ref  string
		role modgraph.GraphRole
	}
	groups := map[key]*BrokenLinkGroup{}
	for _, l := range g.BrokenLinks() {
		k := key{ref: strings.ToLower(l.Ref), role: l.Role}
		grp := groups[k]
		if grp == nil {
			grp = &BrokenLinkGroup{Ref: l.Ref, Role: l.Role}
			groups[k] = grp
		}
		grp.Holders = append(grp.Holders, l.From)
	}
	out := make([]BrokenLinkGroup, 0, len(groups))
	for _, grp := range groups {
		sort.Strings(grp.Holders)
		out = append(out, *grp)
	}
	sort.Slice(out, func(i, j int) bool {
		if len(out[i].Holders) != len(out[j].Holders) {
			return len(out[i].Holders) > len(out[j].Holders)
		}
		return out[i].Ref < out[j].Ref
	})
	return out
}

// ClipName shortens a name to fit a column, with an ellipsis where it was cut.
// A file name that is too long to show is still a name, and a truncated one
// with no mark reads as a different one.
func ClipName(s string, n int) string {
	if n <= 1 || len([]rune(s)) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}

// GraphLinkBatch is a slice of the scan's results. done marks the last one.
type GraphLinkBatch = uistate.GraphLinkBatch

// GraphLinkBatchSize is how many links one batch carries. The links of one
// unit arrive together, so a unit is never split across two batches.
const GraphLinkBatchSize = 128

// stopGraphLinkScan ends a running scan. The goroutine notices through its
// stop channel and returns, so a module reload does not leave one reading a
// tree that is being closed underneath it.
func StopGraphLinkScan(ui *uistate.State) {
	if ui.GraphLinksStop != nil {
		close(ui.GraphLinksStop)
		ui.GraphLinksStop = nil
	}
	ui.GraphLinksRunning = false
	ui.GraphLinksCh = nil
}

// pollGraphLinks takes whatever the scan has finished and reports whether
// anything new arrived, so the caller can ask for a redraw.
func PollGraphLinks(ui *uistate.State) bool {
	if ui.GraphLinksCh == nil {
		return false
	}
	got := false
	for {
		select {
		case batch := <-ui.GraphLinksCh:
			if ui.GraphLinksGraph != nil {
				ui.GraphLinksGraph.AddScannedLinks(batch.Links)
			}
			if batch.Done {
				if ui.GraphLinksGraph != nil {
					ui.GraphLinksGraph.FinishLinkScan()
				}
				StopGraphLinkScan(ui)
			}
			got = true
		default:
			return got
		}
	}
}
