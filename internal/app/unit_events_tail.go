package app

import (
	"dow-mod-studio/internal/whe"
	"gioui.org/layout"
	"gioui.org/widget"
	"strings"
)

// UnitTailInput owns the two editable parts of the selected timeline's motion
// tail — how the clip plays, which the Events panel used to only display. The
// type chip cycles the three types the engine defines on a press; the modifier
// field takes a typed or pasted name and commits it on Enter or on the Set
// beside it. Both write into the in-memory document the way an event value
// does, so Save .whe is what puts them into the mod.
func UnitTailInput(ui *uiState, gtx layout.Context) {
	tl := SelectedUnitEventTimeline(ui)
	if tl == nil || !tl.Tail.Present {
		ui.UnitTailBound = ""
		return
	}
	// Moving to another timeline refills the field from that motion, so the
	// name shown is the one the file holds rather than the last one typed.
	bound := ui.UnitEvtPath + "|" + tl.Name
	if ui.UnitTailBound != bound {
		ui.UnitTailModEd.SetText(tl.Tail.Modifier)
		ui.UnitTailBound = bound
	}
	ui.UnitTailModEd.SingleLine = true
	ui.UnitTailModEd.Submit = true
	submit := false
	for {
		ev, ok := ui.UnitTailModEd.Update(gtx)
		if !ok {
			break
		}
		if _, isSubmit := ev.(widget.SubmitEvent); isSubmit {
			submit = true
		}
	}
	if submit || ui.UnitTailModSet.Clicked(gtx) {
		ApplyUnitTimelineModifier(ui, ui.UnitTailModEd.Text())
	}
	if ui.UnitTailType.Clicked(gtx) {
		ApplyUnitTimelineType(ui, whe.NextMotionType(tl.Tail.Type))
	}
}

// ApplyUnitTimelineType moves the selected timeline's motion to another type.
func ApplyUnitTimelineType(ui *uiState, kind int32) {
	tl := SelectedUnitEventTimeline(ui)
	if tl == nil {
		return
	}
	tail := tl.Tail
	tail.Type = kind
	applyUnitTimelineTail(ui, tail)
}

// ApplyUnitTimelineModifier writes the given modifier name into the motion, an
// empty name clearing it: a motion that drove a modifier and no longer does has
// to store that as the absent flag, not as a name nothing defines.
func ApplyUnitTimelineModifier(ui *uiState, modifier string) {
	tl := SelectedUnitEventTimeline(ui)
	if tl == nil {
		return
	}
	tail := tl.Tail
	tail.Modifier = strings.TrimSpace(modifier)
	applyUnitTimelineTail(ui, tail)
}

// applyUnitTimelineTail writes the tail into the document in memory, marks the
// file unsaved and reports it, the way an event value edit does.
func applyUnitTimelineTail(ui *uiState, tail whe.FxMotionTail) {
	if ui.UnitEvtDoc == nil {
		ui.StatusText = "No .whe is loaded"
		return
	}
	name := ui.UnitEvtTimeline
	if err := ui.UnitEvtDoc.SetTimelineTail(name, tail); err != nil {
		ui.StatusText = "Edit failed: " + err.Error()
		return
	}
	ui.UnitEvtDirty = true
	// The field is refilled from what the payload now holds, so the value on
	// screen is the stored one even when the name was trimmed.
	ui.UnitTailBound = ""
	ui.StatusText = "Edited motion " + name + " — Save .whe to write it into the mod"
	ReloadUnitModelFx(ui)
}
