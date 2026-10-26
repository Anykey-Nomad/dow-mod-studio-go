package app

import (
	"fmt"
	"image"
	"strings"

	"dow-mod-studio/internal/rgd"
	"gioui.org/font"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

// Writing a new object into the mod.
//
// The templates say what a unit or a building file contains; this says where it
// goes and what happens afterwards. The file is written through the mod's own
// overlay, the same way saving an edited RGD is, so it lands in the current
// module folder and the mod tree knows about it straight away — which is what
// makes the new object appear in the object window without a reload.

// newObjKind is what the dialog is being opened to create.
type newObjKind int

const (
	// newObjUnit is a unit: a file under the race's troops folder.
	newObjUnit newObjKind = iota
	// newObjBuilding is a building: a file under the race's structures
	// folder, with the sections a building has rather than a unit's.
	newObjBuilding
)

// newObjNoun is what the dialog calls the thing it is about to write.
func (k newObjKind) newObjNoun() string {
	if k == newObjBuilding {
		return "building"
	}
	return "unit"
}

// newObjPath is where an object of this kind and name would be written.
func (k newObjKind) newObjPath(race, name string) string {
	if k == newObjBuilding {
		return structureTemplateName{Race: race, Name: name}.StructureTemplateVirtualPath()
	}
	return unitTemplateName{Race: race, Name: name}.UnitTemplateVirtualPath()
}

// newObjTemplateNameError is why a name cannot become an object file, said in
// the dialog's own terms rather than as a file system error the user cannot act
// on.
func newObjTemplateNameError(kind newObjKind, name string) error {
	noun := kind.newObjNoun()
	trimmed := strings.TrimSpace(name)
	switch {
	case trimmed == "":
		return fmt.Errorf("the %s needs a name", noun)
	case trimmed != name:
		return fmt.Errorf("the name has a space at either end of it")
	case strings.ContainsAny(name, `\/:*?"<>|`):
		return fmt.Errorf("a %s name cannot hold \\ / : * ? \" < > or |", noun)
	case strings.Contains(name, "."):
		return fmt.Errorf("write the name without .rgd — the extension is added for you")
	}
	for _, r := range name {
		if r < 0x20 {
			return fmt.Errorf("a %s name cannot hold control characters", noun)
		}
	}
	return nil
}

// createNewObject writes a new unit or building of the given race from the
// template and returns the virtual path it wrote.
//
// It refuses rather than guesses: a name the file system would not take, and a
// name that already names a file of the mod or of the game, are both errors the
// dialog shows, because an object that overwrote somebody's would be worse than
// one that was not created.
func (ui *uiState) createNewObject(kind newObjKind, race, name string) (string, error) {
	if ui.modTree == nil {
		return "", fmt.Errorf("no module is loaded")
	}
	if err := newObjTemplateNameError(kind, name); err != nil {
		return "", err
	}
	if race == "" {
		return "", fmt.Errorf("no race is selected, so there is no folder to write the %s into", kind.newObjNoun())
	}
	virtual := kind.newObjPath(race, name)
	if ui.modTree.FindFile(virtual) != nil {
		return "", fmt.Errorf("%s already exists", name)
	}
	var file *rgd.RgdFile
	if kind == newObjBuilding {
		file = newStructureRgd(structureTemplateName{Race: race, Name: name})
	} else {
		file = newUnitRgd(unitTemplateName{Race: race, Name: name})
	}
	data, err := file.MarshalBinary()
	if err != nil {
		return "", fmt.Errorf("the %s could not be written: %v", kind.newObjNoun(), err)
	}
	if _, err := ui.modTree.WriteOverlayFile(virtual, data); err != nil {
		return "", fmt.Errorf("the %s could not be saved: %v", kind.newObjNoun(), err)
	}
	// The catalogue is a snapshot of the tree, so it is thrown away and read
	// again: that is what puts the new object in the list and in the pickers.
	ui.unitCatalog = nil
	ui.unitDetails = nil
	ui.unitDetailsPath = ""
	ui.selectNewObject(race, virtual)
	ui.statusText = fmt.Sprintf("Created %s", name)
	ui.markStateDirty()
	return virtual, nil
}

// selectNewObject points the object window at the file that was just written,
// so the list opens on it rather than where it was before.
func (ui *uiState) selectNewObject(race, virtual string) {
	cat := ui.unitCatalogFor()
	if cat == nil {
		return
	}
	for ri, r := range cat.Races {
		if !strings.EqualFold(r.Name, race) {
			continue
		}
		ui.unitRaceSel = ri
		ui.unitSel = 0
		// unitsOf is the list's own order: the race's troops, then its
		// structures. A new building is picked out of the second half, and a
		// new unit out of the first, without either case being written twice.
		for i, e := range r.UnitsOf() {
			if strings.EqualFold(e.Path, virtual) {
				ui.unitSel = i
				return
			}
		}
		return
	}
}

// ─── merged from object_new_ui.go ───────────────────────────────────────────────

// The New object dialog.
//
// It is a modal over the whole window rather than a panel of the object window,
// because it writes a file: the user should not be able to click their way past
// it and forget what it was about. It says what it is going to do — the race the
// object will belong to and the folder it will be written into — because the one
// thing a generated unit or building cannot work out for itself is which race
// asked for it.
//
// It is one dialog for both kinds rather than two, because the only thing that
// differs is the word on the title, the folder in the path and the template the
// file is built from. Everything else — the name field, the refusal, the way out
// — is the same question asked twice.

// openNewObjDialog puts the dialog up to create a unit or a building of the
// given kind, with the name field ready and empty. The race is the one the
// object window was showing: the object belongs where the user was looking, and
// making them choose again would be a second question with the same answer.
func (ui *uiState) openNewObjDialog(kind newObjKind) {
	race := ui.currentRace()
	if race == nil {
		ui.statusText = "Choose a race before creating a " + kind.newObjNoun()
		return
	}
	ui.closeUnitPickers()
	ui.newObjDialog = true
	ui.newObjKind = kind
	ui.newObjStatus = ""
	ui.newObjNameEditor.SetText("")
}

// closeNewObjDialog puts the dialog away, and the name with it: a name left in
// the field would be offered again as though it were a fresh one.
func (ui *uiState) closeNewObjDialog() {
	ui.newObjDialog = false
	ui.newObjStatus = ""
	ui.newObjNameEditor.SetText("")
}

// newObjFolderLabel is where a new object of the current race and kind will be
// written.
func (ui *uiState) newObjFolderLabel() string {
	race := ui.currentRace()
	if race == nil {
		return ""
	}
	return ui.newObjKind.newObjPath(race.Name, "name")
}

// layoutNewObjDialog draws the dialog as an overlay over the whole window: a
// catcher that takes the click meant for the window behind it, the card, and
// nothing else.
func (ui *uiState) layoutNewObjDialog(gtx layout.Context) layout.Dimensions {
	cardW := gtx.Dp(unit.Dp(420))
	cardH := gtx.Dp(unit.Dp(228))
	// Recorded for tests, the same as the picker list: without it a test has
	// to guess where the card landed and probes the window around it.
	ui.newObjCardBox = image.Rect(gtx.Constraints.Max.X/2-cardW/2, gtx.Constraints.Max.Y/2-cardH/2, gtx.Constraints.Max.X/2+cardW/2, gtx.Constraints.Max.Y/2+cardH/2)
	// A Stack puts a child smaller than itself at the given corner, and its
	// zero value is the top left, so the card is centred by naming the
	// alignment rather than by wrapping it: layout.Center centres within the
	// constraints' minimum, which a stack's children do not carry.
	return layout.Stack{Alignment: layout.Center}.Layout(gtx,
		// The catcher first, so the card is painted over it and takes the
		// clicks meant for itself. It has to be a Stacked child: a Stack lays
		// its Expanded children out last, which would paint the catcher over
		// the card and swallow every click meant for the card.
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return ui.newObjDismissClick.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Dimensions{Size: gtx.Constraints.Max}
			})
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min = image.Pt(cardW, cardH)
			gtx.Constraints.Max = image.Pt(cardW, cardH)
			return widget.Border{Color: cBorder, Width: unit.Dp(1), CornerRadius: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Stack{}.Layout(gtx,
					layout.Expanded(func(gtx layout.Context) layout.Dimensions {
						rrect(gtx, cPanelBg, gtx.Constraints.Max, gtx.Dp(4))
						return layout.Dimensions{Size: gtx.Constraints.Max}
					}),
					layout.Stacked(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Top: unit.Dp(16), Bottom: unit.Dp(16), Left: unit.Dp(16), Right: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									l := material.Label(ui.th, unit.Sp(14), "New "+ui.newObjKind.newObjNoun())
									l.Font.Weight = font.Medium
									l.Color = cText
									return l.Layout(gtx)
								}),
								layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									l := material.Label(ui.th, unit.Sp(11), "Written to "+ui.newObjFolderLabel())
									l.Color = cTextDim
									return l.Layout(gtx)
								}),
								layout.Rigid(layout.Spacer{Height: unit.Dp(10)}.Layout),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									h := gtx.Dp(unit.Dp(28))
									gtx.Constraints.Min.Y = h
									gtx.Constraints.Max.Y = h
									return widget.Border{Color: cBorderLight, Width: unit.Dp(1), CornerRadius: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											ed := material.Editor(ui.th, &ui.newObjNameEditor, "")
											ed.TextSize = unit.Sp(11)
											ed.HintColor = cTextDim
											ed.Hint = ui.newObjHint()
											return ed.Layout(gtx)
										})
									})
								}),
								layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									if ui.newObjStatus == "" {
										return layout.Dimensions{}
									}
									l := material.Label(ui.th, unit.Sp(10), ui.newObjStatus)
									l.Color = cTreeThis
									return l.Layout(gtx)
								}),
								// The buttons take the space left over and sit along its bottom
								// edge. They cannot be a rigid row below a flexible spacer: a
								// Flex lays out every rigid child before the flexible ones, so
								// such a row would sit right under the status line and leave
								// the card's foot empty.
								layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
									return layout.Flex{Axis: layout.Horizontal, Alignment: layout.End}.Layout(gtx,
										layout.Flexed(1, flexGap()),
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											return ui.dialogButton(gtx, &ui.newObjCancel, "Cancel", false)
										}),
										layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											return ui.dialogButton(gtx, &ui.newObjOk, "Create", true)
										}),
									)
								}),
							)
						})
					}),
				)
			})
		}),
	)
}

// newObjHint is the example name the empty field shows. It is a real name from
// the game, so it reads as a name rather than as a placeholder.
func (ui *uiState) newObjHint() string {
	if ui.newObjKind == newObjBuilding {
		return "chaos_hq"
	}
	return "chaos_hound"
}

// handleNewObjDialogInput owns the dialog while it is up. It is read before
// everything else, so a click on the window behind it reaches nothing.
func (ui *uiState) handleNewObjDialogInput(gtx layout.Context) {
	submitted := false
	for {
		ev, ok := ui.newObjNameEditor.Update(gtx)
		if !ok {
			break
		}
		if _, isSubmit := ev.(widget.SubmitEvent); isSubmit {
			submitted = true
		}
	}
	// A click on the dimmed window behind the card is a way out, the same as
	// Escape: both say "not now" and change nothing.
	if ui.newObjDismissClick.Clicked(gtx) && !ui.newObjOk.Clicked(gtx) && !ui.newObjCancel.Clicked(gtx) {
		ui.closeNewObjDialog()
		return
	}
	if ui.newObjCancel.Clicked(gtx) {
		ui.closeNewObjDialog()
		return
	}
	if !ui.newObjOk.Clicked(gtx) && !submitted {
		// Escape leaves the dialog, which a dialog the keyboard can reach has
		// to allow.
		for {
			ev, ok := gtx.Event(key.Filter{Name: key.NameEscape})
			if !ok {
				break
			}
			if e, ok := ev.(key.Event); ok && e.State == key.Press {
				ui.closeNewObjDialog()
				return
			}
		}
		return
	}
	race := ui.currentRace()
	if race == nil {
		ui.newObjStatus = "No race is selected, so there is nowhere to write the " + ui.newObjKind.newObjNoun()
		return
	}
	virtual, err := ui.createNewObject(ui.newObjKind, race.Name, ui.newObjNameEditor.Text())
	if err != nil {
		// A refused name is said in the dialog and the dialog stays up, so the
		// name can be corrected instead of retyped.
		ui.newObjStatus = err.Error()
		return
	}
	ui.closeNewObjDialog()
	// The file is written; opening it is what makes the next step — the model,
	// the icon, the name id — a click away rather than a search through the mod.
	ui.openRgd(virtual)
}
