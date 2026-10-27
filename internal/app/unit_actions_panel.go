package app

import (
	"dow-mod-studio/internal/app/uikit"
	"dow-mod-studio/internal/whe"
	"fmt"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"strconv"
	"strings"
)

// pasteUnitAction writes the copied action, its timelines and its sub-actions
// into this unit's .whe. The timelines go in first — created when this file has
// never had them, merged into the ones it has — so the action's motions resolve
// the moment it lands, then the sub-actions, and the action itself last.
//
// An action of the same name here is refused before anything is written rather
// than merged: an action's choices are an if / else chain in file order, and
// splicing one into another would change what this unit plays.
func PasteUnitAction(ui *uiState) {
	entry, _, ok := ui.UnitClips.Pick(clipActions, ui.UnitClipCur)
	if !ok || entry.Actions == nil {
		ui.StatusText = "The action clipboard is empty — Copy an action first"
		return
	}
	doc := ui.UnitEvtDoc
	if doc == nil {
		ui.StatusText = "No .whe is loaded — select a unit whose model has one first"
		return
	}
	clip := entry.Actions
	if actionIndexByName(doc, clip.Action.Name) >= 0 {
		ui.StatusText = fmt.Sprintf("action %s already exists in this .whe — reconcile it by hand instead of pasting over it",
			clip.Action.Name)
		return
	}
	createdTl, mergedTl, events, anims := 0, 0, 0, 0
	firstErr := ""
	for i := range clip.Timelines {
		res, err := pasteTimelineClip(doc, &clip.Timelines[i])
		if err != nil {
			if firstErr == "" {
				firstErr = err.Error()
			}
			continue
		}
		if res.Created {
			createdTl++
		} else {
			mergedTl++
		}
		events += res.Events
		anims += res.Anims
	}
	addedSubs, presentSubs := 0, 0
	for _, sub := range clip.Subacts {
		if actionIndexByName(doc, sub.Name) >= 0 {
			// The target has its own version of that action: keep it, and let
			// the reader settle the two by hand.
			presentSubs++
			continue
		}
		if err := doc.AddAction(sub); err != nil {
			if firstErr == "" {
				firstErr = err.Error()
			}
			continue
		}
		addedSubs++
	}
	if err := doc.AddAction(clip.Action); err != nil {
		// The timelines and sub-actions may already have landed, and Save .whe
		// has to know the document no longer matches the file.
		if createdTl+mergedTl+addedSubs > 0 {
			ui.UnitEvtDirty = true
			ui.UnitActCondBound = ""
		}
		ui.StatusText = "Paste failed: " + err.Error()
		return
	}
	ui.UnitEvtDirty = true
	ui.UnitActCondBound = ""
	// The pasted action is what the list should show as picked, so the panel
	// below it describes the thing that just landed.
	if i := actionIndexByName(doc, clip.Action.Name); i >= 0 {
		ui.UnitEvtActionSel = i
	}
	ReloadUnitModelFx(ui)
	msg := fmt.Sprintf("Pasted action %s", clip.Action.Name)
	if createdTl+mergedTl > 0 {
		msg += fmt.Sprintf(" with %d timeline(s) (%d new, %d merged, %d event(s)", createdTl+mergedTl, createdTl, mergedTl, events)
		if anims > 0 {
			msg += fmt.Sprintf(", %d animation(s)", anims)
		}
		msg += ")"
	}
	if addedSubs > 0 {
		msg += fmt.Sprintf(" and %d sub-action(s)", addedSubs)
	}
	if presentSubs > 0 {
		msg += fmt.Sprintf(", %d sub-action(s) already here", presentSubs)
	}
	if firstErr != "" {
		msg += ", some skipped: " + firstErr
	}
	ui.StatusText = msg + " — Save .whe to write it in"
}

// unitActConditionsInput runs the presses and typing of the action panel: the
// guard chips cycle a choice's if / else if / else / always step, the
// condition and name fields take the typing, Apply or Enter commits them,
// and the chain tools add, move and delete choices around the caret.
func UnitActConditionsInput(ui *uiState, gtx layout.Context) {
	EnsureUnitActConditions(ui)
	UnitActUpdateNameEditor(ui, gtx)
	if ui.UnitActRename.Clicked(gtx) {
		RenameSelectedUnitAction(ui)
	}
	for i := range ui.UnitActGuardClicks {
		if i >= len(ui.UnitActCondStaged) || !ui.UnitActGuardClicks[i].Clicked(gtx) {
			continue
		}
		ui.UnitActSel = i
		cmp := ui.UnitActCondStaged[i]
		if cmp < whe.FxCompareIf || cmp >= whe.FxCompareNone {
			// A press starts the chain over from if — including for a compare
			// value outside the four the format defines.
			ui.UnitActCondStaged[i] = whe.FxCompareIf
			continue
		}
		ui.UnitActCondStaged[i] = cmp + 1
	}
	for i := range ui.UnitActCondEds {
		UnitActUpdateEditor(ui, gtx, &ui.UnitActCondEds[i], i)
	}
	for i := range ui.UnitActNameEds {
		UnitActUpdateEditor(ui, gtx, &ui.UnitActNameEds[i], i)
	}
	// Whichever field holds the caret is the selected choice: the move, add
	// and delete buttons of the footer act on it.
	for i := range ui.UnitActCondEds {
		if ui.UnitActCondEds[i].Focused(gtx) || (i < len(ui.UnitActNameEds) && ui.UnitActNameEds[i].Focused(gtx)) {
			ui.UnitActSel = i
		}
	}
	if ui.UnitActApply.Clicked(gtx) {
		ApplyUnitActConditions(ui)
	}
	if ui.UnitActAddIf.Clicked(gtx) {
		UnitActAddChoice(ui, actAddIf)
	}
	if ui.UnitActAddElseIf.Clicked(gtx) {
		UnitActAddChoice(ui, actAddElseIf)
	}
	if ui.UnitActAddElse.Clicked(gtx) {
		UnitActAddChoice(ui, actAddElse)
	}
	if ui.UnitActMoveUp.Clicked(gtx) {
		UnitActMoveChoice(ui, -1)
	}
	if ui.UnitActMoveDown.Clicked(gtx) {
		UnitActMoveChoice(ui, 1)
	}
	if ui.UnitActDelete.Clicked(gtx) {
		UnitActDeleteChoice(ui)
	}
}

// unitActUpdateNameEditor owns the action's own name field: a click takes the
// caret, Enter renames, and the field refills from the picked action whenever
// the selection moves — but not while it holds the caret, so what is being
// typed is never overwritten.
func UnitActUpdateNameEditor(ui *uiState, gtx layout.Context) {
	ed := &ui.UnitActNameEd
	ed.SingleLine = true
	ed.Submit = true
	if RgdEnsureEditorClick(ui, ed).Click.Clicked(gtx) {
		gtx.Execute(key.FocusCmd{Tag: ed})
	}
	if !ed.Focused(gtx) {
		var act whe.FxAction
		have := false
		if ui.UnitEvtDoc != nil {
			if a := SelectedUnitEvtAction(ui, ui.UnitEvtDoc.Actions()); a != nil {
				act = *a
				have = true
			}
		}
		bound := strconv.Itoa(ui.UnitEvtActionSel) + "|" + act.Name
		if bound != ui.UnitActNameBound {
			ui.UnitActNameBound = bound
			if have {
				ed.SetText(act.Name)
			} else {
				ed.SetText("")
			}
		}
	}
	for {
		ev, ok := ed.Update(gtx)
		if !ok {
			break
		}
		if _, submit := ev.(widget.SubmitEvent); submit {
			RenameSelectedUnitAction(ui)
			ed.Blur(gtx)
		}
	}
}

// renameSelectedUnitAction renames the picked action in the in-memory
// document. The rename also follows the name into every sub-action link of the
// file that called it, so a chain another action drives keeps working; a name
// another action already has is refused by the document rather than merged.
// Save .whe writes it into the mod.
func RenameSelectedUnitAction(ui *uiState) {
	doc := ui.UnitEvtDoc
	if doc == nil {
		ui.UnitActErr = "No .whe is loaded."
		return
	}
	acts := doc.Actions()
	if ui.UnitEvtActionSel < 0 || ui.UnitEvtActionSel >= len(acts) {
		ui.UnitActErr = "Select an action to rename."
		return
	}
	old := acts[ui.UnitEvtActionSel].Name
	next := strings.TrimSpace(ui.UnitActNameEd.Text())
	if next == "" {
		ui.UnitActErr = "An action needs a name."
		return
	}
	if strings.EqualFold(next, old) {
		ui.UnitActErr = ""
		ui.StatusText = "The action is already called " + old
		return
	}
	if err := doc.RenameAction(old, next); err != nil {
		ui.UnitActErr = err.Error()
		return
	}
	ui.UnitActErr = ""
	ui.UnitEvtDirty = true
	// The conditions and the name field refill from what was just written.
	ui.UnitActCondBound = ""
	ui.UnitActNameBound = ""
	if i := actionIndexByName(doc, next); i >= 0 {
		ui.UnitEvtActionSel = i
	}
	ReloadUnitModelFx(ui)
	ui.StatusText = fmt.Sprintf("Renamed action %s to %s — Save .whe to write it into the mod", old, next)
}

// unitActUpdateEditor processes one field of a choice line: a click takes
// the caret and selects that line, and Enter commits the conditions the way
// Apply does, never dropping a line break into the value.
func UnitActUpdateEditor(ui *uiState, gtx layout.Context, ed *widget.Editor, row int) {
	ed.SingleLine = true
	ed.Submit = true
	if RgdEnsureEditorClick(ui, ed).Click.Clicked(gtx) {
		ui.UnitActSel = row
		gtx.Execute(key.FocusCmd{Tag: ed})
	}
	for {
		ev, ok := ed.Update(gtx)
		if !ok {
			break
		}
		if _, submit := ev.(widget.SubmitEvent); submit {
			ui.UnitActSel = row
			ApplyUnitActConditions(ui)
			ed.Blur(gtx)
		}
	}
}

// unitActPanelFooter is the bottom of the right column while an action is
// being worked on: the commit for the condition fields above, and the same
// Save .whe / Revert pair the timeline form carries — an action edit is
// written to the file exactly the way a timeline edit is.
func UnitActPanelFooter(ui *uiState, gtx layout.Context) layout.Dimensions {
	hasAction := false
	rows := 0
	selName := ""
	if ui.UnitEvtDoc != nil {
		if a := SelectedUnitEvtAction(ui, ui.UnitEvtDoc.Actions()); a != nil {
			hasAction = true
			rows = len(a.Motions) + len(a.Subactions)
			if ui.UnitActSel >= 0 && ui.UnitActSel < rows {
				if list, j, _, ok := actLocate(*a, ui.UnitActSel); ok {
					selName = list[j].Name
				}
			}
		}
	}
	var children []layout.FlexChild
	if ui.UnitActErr != "" {
		err := ui.UnitActErr
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.Th, unit.Sp(9), err)
			l.Color = rgb(0xC98A8A)
			l.MaxLines = 4
			return layout.Inset{Bottom: unit.Dp(4)}.Layout(gtx, l.Layout)
		}))
	}
	// The chain tools act on the selected choice — whichever line held the
	// caret last — and keep the if / else if / else shape intact: a head
	// moves with its whole chain, a continuation slides inside its own.
	selLabel := ""
	if selName != "" {
		selLabel = "selected:  " + unitViewportRowText(selName)
	} else if hasAction && rows > 0 {
		selLabel = "pick a line first"
	}
	children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return SmallButtonDisabled(ui, gtx, &ui.UnitActAddIf, "Add if", !hasAction)
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return SmallButtonDisabled(ui, gtx, &ui.UnitActAddElseIf, "+elif", !hasAction)
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return SmallButtonDisabled(ui, gtx, &ui.UnitActAddElse, "+else", !hasAction)
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return SmallButtonDisabled(ui, gtx, &ui.UnitActMoveUp, "\u25b2", !UnitActMoveEnabled(ui, -1))
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return SmallButtonDisabled(ui, gtx, &ui.UnitActMoveDown, "\u25bc", !UnitActMoveEnabled(ui, 1))
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return SmallButtonDisabled(ui, gtx, &ui.UnitActDelete, "Delete", rows == 0)
				}),
				layout.Flexed(1, rowGap()),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					l := material.Label(ui.Th, unit.Sp(9), selLabel)
					l.Color = uikit.CTextDim
					l.MaxLines = 1
					return l.Layout(gtx)
				}),
			)
		})
	}))
	children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return SmallButtonDisabled(ui, gtx, &ui.UnitActApply, "Apply", !hasAction)
			}),
			layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return PrimaryButton(ui, gtx, &ui.UnitEvtSave, "Save .whe")
			}),
			layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return SmallButtonDisabled(ui, gtx, &ui.UnitEvtRevert, "Revert", !ui.UnitEvtDirty)
			}),
		)
	}))
	note := "Saved into this mod's own .whe; the game's files are never written."
	if ui.UnitEvtDirty {
		note = "Unsaved changes. " + note
	}
	children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return unitFieldNote(gtx, ui.Th, note)
		})
	}))
	inset := layout.Inset{Top: unit.Dp(8), Left: unit.Dp(8), Right: unit.Dp(8), Bottom: unit.Dp(8)}
	return inset.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	})
}

// unitEvtActionPanelBody is the right column while an action is being worked
// on: that action's own properties — its name and the choices the game walks
// when it runs, each guarded by its step of the if / else if / else chain —
// and none of the timeline's events. The panel shows one or the other, so a
// timeline being edited gives the whole panel to its events instead.
func UnitEvtActionPanelBody(ui *uiState, gtx layout.Context) []layout.FlexChild {
	acts := ui.UnitEvtDoc.Actions()
	a := SelectedUnitEvtAction(ui, acts)
	if a == nil {
		return []layout.FlexChild{
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return UnitEventsMessage(ui, gtx, "This .whe carries no actions.")
			}),
		}
	}
	act := *a
	text := "Action  " + act.Name
	if ui.UnitEvtDirty {
		text += "  • unsaved"
	}
	return []layout.FlexChild{
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.Th, unit.Sp(9), text)
			l.Color = uikit.CTextDim
			l.MaxLines = 2
			return layout.Inset{Bottom: unit.Dp(6)}.Layout(gtx, l.Layout)
		}),
		// The action's name is its identity across the file, and it is what a
		// paste from another unit lands on — so it can be changed here, which
		// is how a name this .whe already uses is settled without editing the
		// file by hand.
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Max.X = gtx.Dp(112)
					l := material.Label(ui.Th, unit.Sp(9), "Name")
					l.Color = uikit.CTextDim
					l.MaxLines = 1
					return l.Layout(gtx)
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Min.Y = gtx.Dp(20)
					gtx.Constraints.Max.Y = gtx.Dp(22)
					b := widget.Border{Color: uikit.CBorderLight, Width: unit.Dp(1), CornerRadius: unit.Dp(2)}
					return b.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Left: unit.Dp(4), Right: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							w := material.Editor(ui.Th, &ui.UnitActNameEd, act.Name)
							w.TextSize = unit.Sp(10)
							w.HintColor = uikit.CTextDim
							return w.Layout(gtx)
						})
					})
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return SmallButton(ui, gtx, &ui.UnitActRename, "Rename")
				}),
			)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return sectionHeader(gtx, ui.Th, "Conditions")
			})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.Th, unit.Sp(9), "One line per choice — press a guard to cycle it, type the condition inline. Apply or Enter writes the document; Save .whe writes the file.")
			l.Color = uikit.CTextDim
			l.MaxLines = 3
			return layout.Inset{Bottom: unit.Dp(6)}.Layout(gtx, l.Layout)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return UnitActionConditions(ui, gtx, act)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.Th, unit.Sp(9), "Pick a timeline above to edit its events instead.")
			l.Color = uikit.CTextDim
			l.MaxLines = 2
			return layout.Inset{Top: unit.Dp(8)}.Layout(gtx, l.Layout)
		}),
	}
}
