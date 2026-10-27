package unit

import (
	"dow-mod-studio/internal/app/uikit"
	"dow-mod-studio/internal/app/uistate"
	"dow-mod-studio/internal/app/viewer"
	"dow-mod-studio/internal/unitdef"
	"dow-mod-studio/internal/whm"
	"gioui.org/font"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"image/color"
	"sort"
	"strings"
)

// UnitPickerStripHeightDp is the strip's own height, used to keep the dropdowns
// from overlapping the columns when they open.
const UnitPickerStripHeightDp = 34

// UnitTabPadDp is the padding around the columns and the strip in the tab's
// content area.
const UnitTabPadDp = 8

// UnitListVisibleRows is how many unit rows the dropdown shows before it
// starts scrolling.
const UnitListVisibleRows = 12

// applyPendingUnitPick puts the pickers back where the last session left them.
//
// They are restored by name rather than by index because the catalog is built
// afresh from whatever the mod holds: an index saved in one session would point
// at whatever unit happens to sit there in the next. A name the new mod does
// not have — a unit that was deleted, or a mod that was swapped since — is
// simply not used, and the window opens on the first unit as it always did.
//
// This is the only place that consumes them, so a mod opened later in the
// session starts from the top rather than being dragged back to a unit of the
// one that was open when the app started.
func ApplyPendingUnitPick(ui *uistate.State) {
	if ui.UnitCatalog == nil || (ui.UnitPickRace == "" && ui.UnitPickPath == "") {
		return
	}
	race, path := ui.UnitPickRace, ui.UnitPickPath
	ui.UnitPickRace, ui.UnitPickPath = "", ""
	for i := range ui.UnitCatalog.Races {
		r := &ui.UnitCatalog.Races[i]
		if race != "" && !strings.EqualFold(r.Name, race) {
			continue
		}
		ui.UnitRaceSel = i
		if path == "" {
			return
		}
		units := r.UnitsOf()
		for j := range units {
			if strings.EqualFold(units[j].Path, path) {
				ui.UnitSel = j
				return
			}
		}
		// The race is here but the unit is not, which is a mod that has
		// changed since: the race is still the right one to open on.
		return
	}
}

// unitPickerField is one labelled dropdown trigger. The label sits in front of
// the value so the strip reads "Race  necrons" without needing a header row.
func UnitPickerField(ui *uistate.State, gtx layout.Context, label, value string, enabled, open bool) layout.Dimensions {
	return FrameMeasured(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8), Top: unit.Dp(3), Bottom: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					l := material.Label(ui.Th, unit.Sp(10), label)
					l.Color = uikit.CTextDim
					return l.Layout(gtx)
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					text := value
					if len(text) > 42 {
						text = text[:39] + "…"
					}
					l := material.Label(ui.Th, unit.Sp(11), text)
					l.Color = uikit.CText
					if !enabled {
						l.Color = uikit.CTextDim
					}
					l.MaxLines = 1
					return l.Layout(gtx)
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					// A caret, drawn as text so it needs no icon font.
					l := material.Label(ui.Th, unit.Sp(9), "▼")
					l.Color = uikit.CTextDim
					return l.Layout(gtx)
				}),
			)
		})
	}, uikit.CSaveBg, UnitPickerBorder(open), unit.Dp(3))
}

// UnitPickerBorder is a picker field's frame colour: the accent colour while
// its list is open, the normal one otherwise.
func UnitPickerBorder(open bool) color.NRGBA {
	if open {
		return uikit.CSelectBd
	}
	return uikit.CSaveBd
}

func EnsureUnitRaceClicks(ui *uistate.State, n int) {
	if len(ui.UnitRaceClicks) != n {
		ui.UnitRaceClicks = make([]widget.Clickable, n)
	}
}

// ensureUnitDropdownClicks sizes the unit dropdown's row click targets. It is
// distinct from the section list's ensureUnitClicks, which takes no count.
func EnsureUnitDropdownClicks(ui *uistate.State, n int) {
	if len(ui.UnitClicks) != n {
		ui.UnitClicks = make([]widget.Clickable, n)
	}
}

// unitColumnBody is the column's shared frame: header, rule, and a body that is
// either the section's list or its hint.
func UnitColumnBody(ui *uistate.State, gtx layout.Context, hint string) layout.Dimensions {
	return uikit.PanelFrame(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return LayoutUnitColumnHeader(ui, gtx, CurrentUnitSection(ui).MiddleTitle)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return uikit.HLine(gtx, uikit.CHeaderLine, 1) }),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(12), Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					l := material.Label(ui.Th, unit.Sp(10), hint)
					l.Color = uikit.CTextDim
					return l.Layout(gtx)
				})
			}),
		)
	})
}

// unitSectionFrame is the frame of a column that carries a list.
func UnitSectionFrame(ui *uistate.State, gtx layout.Context, body layout.Widget) layout.Dimensions {
	return uikit.PanelFrame(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return LayoutUnitColumnHeader(ui, gtx, CurrentUnitSection(ui).MiddleTitle)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return uikit.HLine(gtx, uikit.CHeaderLine, 1) }),
			layout.Flexed(1, body),
		)
	})
}

// UnitInfoRowHeightDp is the row height the info list's scrollbar is drawn at. The
// rows are not all this tall — a localisation row is two lines, a heading is
// another two — so this is the plain row's height and only feeds the thumb's
// size, which is what the panel's other lists do with it too.
const UnitInfoRowHeightDp = 17

// UnitWeaponRow is one selectable row of the weapons or abilities list.
type UnitWeaponRow struct {
	// hardpoint is the owning field, e.g. "hardpoint_01" or "ability_02".
	Hardpoint string
	// name is the weapon's or ability's file name.
	Name string
	// ref is the raw reference as the unit wrote it.
	Ref string
	// path is the file the reference resolved to, "" when it did not.
	Path string
	// found reports whether ref resolved to a file in the tree.
	Found bool
}

// UnitWeaponRows flattens the unit's hardpoints into one row per weapon slot.
func UnitWeaponRows(d *uistate.UnitDetails) []UnitWeaponRow {
	if d == nil {
		return nil
	}
	var out []UnitWeaponRow
	for _, h := range d.Weapons {
		for _, w := range h.Weapons {
			out = append(out, UnitWeaponRow{Hardpoint: h.Hardpoint, Name: w.Name, Ref: w.Ref, Path: w.Path, Found: w.Path != ""})
		}
	}
	return out
}

// UnitAbilityRows flattens the unit's ability slots into rows. A slot with an
// empty value is left out: it is not an ability the unit has, and 115 of the
// race unit .rgds ship an empty ability_01.
func UnitAbilityRows(d *uistate.UnitDetails) []UnitWeaponRow {
	if d == nil {
		return nil
	}
	var out []UnitWeaponRow
	for _, a := range d.Abilities {
		if a.Ref == "" {
			continue
		}
		out = append(out, UnitWeaponRow{Hardpoint: a.Slot, Name: a.Name(), Ref: a.Ref, Path: a.Path, Found: a.Resolved()})
	}
	return out
}

// unitGroupHeading is the dimmed, semibold heading above the first row of a
// group in a properties column — the same shape the info list draws, so a
// grouped column reads the same wherever it appears.
func UnitGroupHeading(ui *uistate.State, gtx layout.Context, text string) layout.Dimensions {
	return layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		l := material.Label(ui.Th, unit.Sp(11), text)
		l.Color = uikit.CTextDim
		l.Font.Weight = font.SemiBold
		l.MaxLines = 1
		return l.Layout(gtx)
	})
}

// UnitWeaponAt is the weapon a row of the Weapons list stands for, matching
// the row back to the hardpoint and slot it came from.
func UnitWeaponAt(d *uistate.UnitDetails, rowIndex int) *unitdef.WeaponSlot {
	if d == nil {
		return nil
	}
	seen := 0
	for hi := range d.Weapons {
		for si := range d.Weapons[hi].Weapons {
			if seen == rowIndex {
				return &d.Weapons[hi].Weapons[si]
			}
			seen++
		}
	}
	return nil
}

// UnitWeaponSlotFields finds the scalar fields of the weapon a row stands for,
// matching the row back to the hardpoint and slot it came from.
func UnitWeaponSlotFields(d *uistate.UnitDetails, rowIndex int) []unitdef.UnitField {
	if w := UnitWeaponAt(d, rowIndex); w != nil {
		return w.Fields
	}
	return nil
}

func EnsureUnitRowClicks(ui *uistate.State, n int) {
	if len(ui.UnitRowClicks) != n {
		ui.UnitRowClicks = make([]widget.Clickable, n)
	}
}

// unitPickerOpen reports whether either dropdown is showing.
func UnitPickerOpen(ui *uistate.State) bool { return ui.UnitRaceOpen || ui.UnitListOpen }

// closeUnitPickers puts both dropdowns away.
func CloseUnitPickers(ui *uistate.State) {
	ui.UnitRaceOpen = false
	ui.UnitListOpen = false
}

// handleUnitPickerKeys puts an open list away on Escape. The key is only read
// while a list is showing, so Escape keeps meaning "close" everywhere else.
func HandleUnitPickerKeys(ui *uistate.State, gtx layout.Context) {
	if !UnitPickerOpen(ui) {
		return
	}
	for {
		ev, ok := gtx.Event(key.Filter{Name: key.NameEscape})
		if !ok {
			return
		}
		e, ok := ev.(key.Event)
		if !ok {
			continue
		}
		if e.State == key.Press {
			CloseUnitPickers(ui)
			return
		}
	}
}

// UnitImpRef names one frame of one animation of the imported file: which
// clip it belongs to and which frame it stands on. The selection is keyed
// this way because several clips can be open at once and a frame number on
// its own says which of them it came from.
type UnitImpRef = uistate.UnitImpRef

// unitImportBoneIndex is the source file's bone matching the picked bone of
// the edited model, by name — the two files are assumed to carry the same
// skeleton, which is the premise of the whole feature. -1 means no match.
func UnitImportBoneIndex(ui *uistate.State, v *viewer.ModelViewer) int {
	if ui.UnitImpModel == nil || v == nil || v.Model == nil || v.BoneIndex < 0 || v.BoneIndex >= len(v.Model.Bones) {
		return -1
	}
	name := v.Model.Bones[v.BoneIndex].Name
	if name == "" {
		return -1
	}
	return ui.UnitImpModel.BoneIndexByName(name)
}

// UnitImportFrames lists the frames the matched bone has keys on in one
// source clip: position and rotation keys together, in frame order, which
// is the same walk the panel's own keyframe list takes over a track. A
// frame carrying only one of the two kinds is still a frame worth copying —
// the note on its row says which kind it holds.
func UnitImportFrames(a *uistate.WhmAnimation, bone int) []int {
	if a == nil || bone < 0 {
		return nil
	}
	var track *whm.Track
	for i := range a.Bones {
		if a.Bones[i].Bone == bone {
			track = &a.Bones[i]
			break
		}
	}
	if track == nil {
		return nil
	}
	seen := make(map[int]bool)
	for i := range track.PosKey {
		seen[viewer.FrameAt(float64(track.PosKey[i].Time), a.NumFrames)] = true
	}
	for i := range track.RotKey {
		seen[viewer.FrameAt(float64(track.RotKey[i].Time), a.NumFrames)] = true
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]int, 0, len(seen))
	for f := range seen {
		out = append(out, f)
	}
	sort.Ints(out)
	return out
}

// UnitImportFrameKinds says which key kinds a source frame carries for the
// matched bone, as the row's note: "pos+rot", "pos" or "rot".
func UnitImportFrameKinds(a *uistate.WhmAnimation, bone, frame int) string {
	if a == nil || bone < 0 {
		return ""
	}
	var track *whm.Track
	for i := range a.Bones {
		if a.Bones[i].Bone == bone {
			track = &a.Bones[i]
			break
		}
	}
	if track == nil {
		return ""
	}
	pos, rot := false, false
	for i := range track.PosKey {
		if viewer.FrameAt(float64(track.PosKey[i].Time), a.NumFrames) == frame {
			pos = true
			break
		}
	}
	for i := range track.RotKey {
		if viewer.FrameAt(float64(track.RotKey[i].Time), a.NumFrames) == frame {
			rot = true
			break
		}
	}
	switch {
	case pos && rot:
		return "pos+rot"
	case pos:
		return "pos"
	case rot:
		return "rot"
	}
	return ""
}

// unitImportRowClick returns the row's click target, allocating it the
// first time a frame is shown. The map rather than a slice: frames appear
// and vanish as clips are opened, and a click target has to stay the same
// widget between frames for a press to complete.
func UnitImportRowClick(ui *uistate.State, ref UnitImpRef) *widget.Clickable {
	if ui.UnitImpFrameClicks == nil {
		ui.UnitImpFrameClicks = make(map[UnitImpRef]*widget.Clickable)
	}
	c := ui.UnitImpFrameClicks[ref]
	if c == nil {
		c = &widget.Clickable{}
		ui.UnitImpFrameClicks[ref] = c
	}
	return c
}

// WriteImportPos puts one source position value on the frame of the picked
// bone's track. It reports whether a key was created and whether an
// existing one was written over.
func WriteImportPos(t *whm.Track, v *viewer.ModelViewer, frame int, val [3]float32, replace bool) (added, replaced int) {
	if idx := viewer.BoneKeyIndexAtFrame(v, frame, 0); idx >= 0 {
		if !replace {
			return 0, 0
		}
		t.PosKey[idx].V = val
		return 0, 1
	}
	now := viewer.TimeForFrame(frame, v.Anim.NumFrames)
	at := 0
	for at < len(t.PosKey) && t.PosKey[at].Time <= now {
		at++
	}
	t.PosKey = append(t.PosKey, uistate.WhmKey3{})
	copy(t.PosKey[at+1:], t.PosKey[at:])
	t.PosKey[at] = uistate.WhmKey3{Time: now, V: val}
	return 1, 0
}

// WriteImportRot is WriteImportPos for rotation keys.
func WriteImportRot(t *whm.Track, v *viewer.ModelViewer, frame int, val [4]float32, replace bool) (added, replaced int) {
	if idx := viewer.BoneKeyIndexAtFrame(v, frame, 1); idx >= 0 {
		if !replace {
			return 0, 0
		}
		t.RotKey[idx].V = val
		return 0, 1
	}
	now := viewer.TimeForFrame(frame, v.Anim.NumFrames)
	at := 0
	for at < len(t.RotKey) && t.RotKey[at].Time <= now {
		at++
	}
	t.RotKey = append(t.RotKey, uistate.WhmKey4{})
	copy(t.RotKey[at+1:], t.RotKey[at:])
	t.RotKey[at] = uistate.WhmKey4{Time: now, V: val}
	return 1, 0
}
